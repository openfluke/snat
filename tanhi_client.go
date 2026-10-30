// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// TelemetrySender sends sample traffic only over TANHI UDP. TCP flush messages
// contain sequence counts/acks; missing samples are retransmitted over UDP.
type TelemetrySender struct {
	control            *Client
	conn               *net.UDPConn
	id, token, address string
	packets            [][]byte
	bytes              int
}

func (c *Client) TANHI(id, token string) (*TelemetrySender, error) {
	host, port, err := net.SplitHostPort(c.UDPAddress)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host, _, err = net.SplitHostPort(c.conn.RemoteAddr().String())
		if err != nil {
			return nil, err
		}
	}
	address := net.JoinHostPort(host, port)
	resolved, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, resolved)
	if err != nil {
		return nil, err
	}
	return &TelemetrySender{control: c, conn: conn, id: id, token: token, address: address}, nil
}
func (s *TelemetrySender) Close() error        { return s.conn.Close() }
func (s *TelemetrySender) Address() string     { return s.address }
func (s *TelemetrySender) StartMarker() string { return "snat:" + s.id + ":" + s.token }
func (s *TelemetrySender) EndMarker() string   { return "snat-end:" + s.id + ":" + s.token }
func (s *TelemetrySender) SendEvent(event Event) error {
	phase := "fwd"
	if event["Type"] == "backward" {
		phase = "update"
	}
	integer := func(key string) int {
		switch n := event[key].(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
		return 0
	}
	now := time.Now().UnixNano()
	packet := TANHIPacket{V: "tanhi1", Seq: uint64(len(s.packets) + 1), Phase: phase, SessionID: s.id, StreamToken: s.token, Event: event, Z: integer("grid_depth"), Y: integer("grid_row"), X: integer("grid_col"), L: integer("cell_layer"), DType: integer("dtype_id"), T0Ns: now, T1Ns: now}
	if stats, ok := event["Stats"].(map[string]any); ok {
		packet.Layer, _ = stats["LayerType"].(string)
	}
	if shape, ok := event["shape"].([]int); ok {
		packet.Shape = shape
	}
	b, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	if len(b) > 60000 {
		return fmt.Errorf("TANHI datagram exceeds 60000 bytes")
	}
	if s.bytes+len(b) > maxDocument {
		return fmt.Errorf("TANHI retry cache exceeds 64 MiB")
	}
	s.bytes += len(b)
	s.packets = append(s.packets, b)
	_, err = s.conn.Write(b)
	return err
}
func (s *TelemetrySender) Flush(ctx context.Context, waitNative bool) (WireReply, error) {
	var reply WireReply
	var err error
	until := time.Now().Add(time.Second)
	for attempt := 0; attempt < 30; attempt++ {
		reply, err = s.control.Send(ctx, WireMessage{Op: "flush", ID: s.id, ExpectedPackets: uint64(len(s.packets))})
		if err != nil {
			return reply, err
		}
		if len(reply.Missing) > 0 {
			for _, seq := range reply.Missing {
				if seq > 0 && seq <= uint64(len(s.packets)) {
					if _, err = s.conn.Write(s.packets[seq-1]); err != nil {
						return reply, err
					}
				}
			}
		}
		if reply.Telemetry.SampleMissing == 0 && (!waitNative || (reply.Telemetry.NativeClosed && reply.Telemetry.NativeMissing == 0)) {
			return reply, nil
		}
		if time.Now().After(until) {
			break
		}
		select {
		case <-ctx.Done():
			return reply, ctx.Err()
		case <-time.After(35 * time.Millisecond):
		}
	}
	if waitNative && !reply.Telemetry.NativeClosed {
		reply.Telemetry.Partial = true
	}
	return reply, nil
}

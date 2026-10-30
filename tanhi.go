// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// TANHIPacket extends Welvet's tanhi1 datagram with session routing and measured
// samples. Native Welvet tanhi1 packets are also accepted after an authenticated
// sweep marker. TCP carries management and acknowledgements, never these values.
type TANHIPacket struct {
	V           string `json:"v"`
	Seq         uint64 `json:"seq"`
	Phase       string `json:"phase"`
	Z           int    `json:"z"`
	Y           int    `json:"y"`
	X           int    `json:"x"`
	L           int    `json:"l"`
	Layer       string `json:"layer"`
	DType       int    `json:"dtype"`
	T0Ns        int64  `json:"t0_ns"`
	T1Ns        int64  `json:"t1_ns"`
	Label       string `json:"label,omitempty"`
	Shape       []int  `json:"shape,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	StreamToken string `json:"stream_token,omitempty"`
	Event       Event  `json:"event,omitempty"`
}
type UDPStats struct {
	Received      uint64 `json:"received"`
	Samples       uint64 `json:"samples"`
	Native        uint64 `json:"native"`
	SampleMissing uint64 `json:"sample_missing"`
	NativeMissing uint64 `json:"native_missing"`
	NativeClosed  bool   `json:"native_closed"`
	Partial       bool   `json:"partial"`
	Rejected      uint64 `json:"rejected"`
}
type nativeBinding struct {
	ID         string
	Start, End uint64
	Seen       map[uint64]bool
}

func (h *liveHub) resetTelemetry(s *Session, token string) {
	h.telemetryErr = ""
	h.stream = s
	h.streamToken = token
	h.sampleThrough = 0
	h.sampleQueue = map[uint64]Event{}
	h.nativeBindings = map[string]*nativeBinding{}
	h.udpStats = UDPStats{}
	h.pending = nil
	for _, e := range s.Events {
		if e["transport_channel"] == "samples" {
			var seq uint64
			switch n := e["tanhi_seq"].(type) {
			case float64:
				seq = uint64(n)
			case uint64:
				seq = n
			}
			if seq > h.sampleThrough {
				h.sampleThrough = seq
			}
		}
	}
	h.udpStats.Samples = h.sampleThrough
}
func (h *liveHub) readUDP(conn *net.UDPConn) {
	defer h.wg.Done()
	defer func() { h.mu.Lock(); diskMu.Lock(); _ = h.flushTelemetryLocked(); diskMu.Unlock(); h.mu.Unlock() }()
	data := make([]byte, 65536)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Millisecond))
		n, source, err := conn.ReadFromUDP(data)
		if err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				h.mu.Lock()
				diskMu.Lock()
				if e := h.flushTelemetryLocked(); e != nil {
					h.telemetryErr = e.Error()
				}
				diskMu.Unlock()
				h.mu.Unlock()
				continue
			}
			return
		}
		if n > 60000 {
			continue
		}
		var packet TANHIPacket
		if json.Unmarshal(data[:n], &packet) != nil || packet.V != "tanhi1" {
			continue
		}
		h.mu.Lock()
		h.acceptTANHILocked(packet, source.String())
		if len(h.pending) >= 64 {
			diskMu.Lock()
			if e := h.flushTelemetryLocked(); e != nil {
				h.telemetryErr = e.Error()
			}
			diskMu.Unlock()
		}
		h.mu.Unlock()
	}
}
func (h *liveHub) acceptTANHILocked(p TANHIPacket, source string) {
	if h.stream == nil || h.stream.Status != "running" {
		return
	}
	if chapter := h.currentChapter(); chapter != nil && chapter.Status != "running" {
		return
	}
	authenticated := func(id, token string) bool {
		return id == h.stream.ID && subtle.ConstantTimeCompare([]byte(token), []byte(h.streamToken)) == 1
	}
	if p.SessionID != "" {
		if !authenticated(p.SessionID, p.StreamToken) || p.Event == nil || p.Seq == 0 {
			h.udpStats.Rejected++
			return
		}
		if p.Seq <= h.sampleThrough {
			return
		}
		if _, seen := h.sampleQueue[p.Seq]; seen {
			return
		}
		if len(h.sampleQueue) >= 4096 {
			h.udpStats.Rejected++
			return
		}
		if chapter := h.currentChapter(); chapter != nil {
			p.Event["chapter_id"] = chapter.ID
		}
		p.Event["tanhi_seq"] = p.Seq
		p.Event["transport_channel"] = "samples"
		p.Event["transport"] = "tanhi_udp"
		h.sampleQueue[p.Seq] = p.Event
		h.udpStats.Received++
		for {
			next := h.sampleThrough + 1
			event, ok := h.sampleQueue[next]
			if !ok {
				break
			}
			delete(h.sampleQueue, next)
			h.pending = append(h.pending, event)
			h.sampleThrough = next
		}
		h.udpStats.Samples = h.sampleThrough
		return
	}
	if p.Phase == "sweep" {
		parts := strings.Split(p.Label, ":")
		if len(parts) != 3 || !authenticated(parts[1], parts[2]) {
			return
		}
		switch parts[0] {
		case "snat":
			if len(h.nativeBindings) >= 64 {
				return
			}
			h.nativeBindings[source] = &nativeBinding{ID: parts[1], Start: p.Seq, Seen: map[uint64]bool{p.Seq: true}}
		case "snat-end":
			if binding := h.nativeBindings[source]; binding != nil && binding.ID == parts[1] {
				binding.End = p.Seq
				binding.Seen[p.Seq] = true
				h.udpStats.NativeClosed = true
			}
		}
		return
	}
	binding := h.nativeBindings[source]
	if binding == nil || binding.ID != h.stream.ID || p.Seq <= binding.Start {
		return
	}
	if binding.Seen[p.Seq] {
		return
	}
	binding.Seen[p.Seq] = true
	direction := "forward"
	if p.Phase == "bwd" {
		direction = "backward"
	}
	e := Event{"Type": direction, "phase": "native_tanhi_" + p.Phase, "values_kind": "runtime_timing", "transport": "tanhi_udp", "transport_channel": "native", "tanhi_seq": p.Seq, "grid_depth": p.Z, "grid_row": p.Y, "grid_col": p.X, "cell_layer": p.L, "layer_id": fmt.Sprintf("grid/%d/%d/%d/%d", p.Z, p.Y, p.X, p.L), "t0_ns": p.T0Ns, "t1_ns": p.T1Ns, "shape": p.Shape, "Stats": map[string]any{"LayerType": p.Layer}}
	if chapter := h.currentChapter(); chapter != nil {
		e["chapter_id"] = chapter.ID
	}
	h.pending = append(h.pending, e)
	h.udpStats.Native++
	h.udpStats.Received++
}
func (h *liveHub) flushTelemetryLocked() error {
	if h.stream == nil || len(h.pending) == 0 {
		return nil
	}
	var state streamState
	if err := readJSON(statePath(h.root, h.stream.ID), &state); err != nil {
		return err
	}
	if state.Status != "running" {
		h.pending = nil
		return nil
	}
	encoded, err := json.Marshal(h.pending)
	if err != nil {
		return err
	}
	if state.Bytes+int64(len(encoded)) > maxDocument {
		return fmt.Errorf("recording exceeds 64 MiB; reduce samples")
	}
	if err = writeJSON(framePath(h.root, h.stream.ID, state.Frames), h.pending); err != nil {
		return err
	}
	state.Frames++
	state.Next++
	state.Revision++
	state.Events += len(h.pending)
	state.Bytes += int64(len(encoded))
	if err = writeJSON(statePath(h.root, h.stream.ID), state); err != nil {
		return err
	}
	h.stream.Events = append(h.stream.Events, h.pending...)
	h.pending = nil
	applyStreamState(h.stream, state)
	h.remember(h.stream)
	h.updated = time.Now().UTC()
	return nil
}
func (h *liveHub) receipt(expected uint64) WireReply {
	result := WireReply{OK: true, UDPAddress: h.udpAddress, Telemetry: h.udpStats}
	if h.stream != nil {
		result.ID = h.stream.ID
		result.EventCount = len(h.stream.Events)
		result.Revision = h.stream.Revision
		result.NextSequence = h.stream.NextSequence
	}
	for seq := h.sampleThrough + 1; seq <= expected; seq++ {
		if _, queued := h.sampleQueue[seq]; !queued {
			result.Telemetry.SampleMissing++
			if len(result.Missing) < 1024 {
				result.Missing = append(result.Missing, seq)
			}
		}
	}
	for _, binding := range h.nativeBindings {
		if binding.End > binding.Start {
			expected := binding.End - binding.Start + 1
			if expected > uint64(len(binding.Seen)) {
				result.Telemetry.NativeMissing += expected - uint64(len(binding.Seen))
			}
		}
	}
	result.Telemetry.Partial = result.Telemetry.SampleMissing > 0 || result.Telemetry.NativeMissing > 0
	if h.telemetryErr != "" {
		result.OK = false
		result.Error = h.telemetryErr
	}
	return result
}

// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client serializes acknowledged frames. Callers can safely retry a failed
// append with the same session ID and sequence after reconnecting.
type Client struct {
	UDPAddress string
	mu         sync.Mutex
	conn       net.Conn
	reader     *bufio.Reader
	token      string
}

func Dial(ctx context.Context, address, token string) (*Client, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, reader: bufio.NewReader(conn), token: token}
	reply, err := c.Send(ctx, WireMessage{Op: "hello"})
	if err != nil {
		conn.Close()
		return nil, err
	}
	c.UDPAddress = reply.UDPAddress
	return c, nil
}
func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) Send(ctx context.Context, m WireMessage) (WireReply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var reply WireReply
	if err := ctx.Err(); err != nil {
		return reply, err
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.conn.SetDeadline(deadline)
	m.Protocol = WireProtocol
	m.Token = c.token
	data, err := json.Marshal(m)
	if err != nil {
		return reply, err
	}
	if len(data)+1 >= MaxWireFrame {
		return reply, fmt.Errorf("SNAT frame exceeds 8 MiB")
	}
	data = append(data, '\n')
	for len(data) > 0 {
		n, e := c.conn.Write(data)
		if e != nil {
			return reply, e
		}
		data = data[n:]
	}
	data, err = c.reader.ReadBytes('\n')
	if err != nil {
		return reply, err
	}
	if len(data) > MaxWireFrame {
		return reply, fmt.Errorf("SNAT reply exceeds 8 MiB")
	}
	if err = json.Unmarshal(data, &reply); err != nil {
		return reply, err
	}
	if !reply.OK {
		return reply, fmt.Errorf("SNAT: %s", reply.Error)
	}
	return reply, nil
}

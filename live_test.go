// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func liveRequest(t *testing.T, root string, r Request) LiveStatus {
	t.Helper()
	raw, _ := json.Marshal(r)
	v, err := Execute(root, raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.(LiveStatus)
}
func TestLiveTCPDurabilitySequenceAndPinnedView(t *testing.T) {
	root := t.TempDir()
	status := liveRequest(t, root, Request{Action: "server_start", Address: "127.0.0.1:0", Token: "test-token"})
	defer StopLive(root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if bad, err := Dial(ctx, status.Address, "wrong"); err == nil {
		bad.Close()
		t.Fatal("accepted wrong token")
	}
	c, err := Dial(ctx, status.Address, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	create := func(key string) string {
		t.Helper()
		r, e := c.Send(ctx, WireMessage{Op: "create", Key: key, Session: &Session{Title: key, Layers: []Layer{{ID: "grid/0/0/0/0", Name: "Dense", Type: "Dense"}}}})
		if e != nil {
			t.Fatal(e)
		}
		return r.ID
	}
	first := create("run/case-one")
	frame := WireMessage{Op: "append", ID: first, Sequence: 0, Events: []Event{{"type": "forward", "layer_id": "grid/0/0/0/0", "values": []float32{.1, .2}}}}
	for i := 0; i < 2; i++ {
		if _, err = c.Send(ctx, frame); err != nil {
			t.Fatal(err)
		}
	}
	bad := frame
	bad.Sequence = 4
	if _, err = c.Send(ctx, bad); err == nil {
		t.Fatal("accepted sequence gap")
	}
	if _, err = c.Send(ctx, WireMessage{Op: "finish", ID: first, Sequence: 1, Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Send(ctx, WireMessage{Op: "import", Key: "/etc/passwd"}); err == nil {
		t.Fatal("TCP exposed file operations")
	}
	liveRequest(t, root, Request{Action: "live_select", ID: first, Follow: false})
	second := create("run/case-two")
	if second == first {
		t.Fatal("duplicate IDs")
	}
	poll := liveRequest(t, root, Request{Action: "live_poll"})
	if poll.Session == nil || poll.Session.ID != first || len(poll.Session.Events) != 1 {
		t.Fatalf("pin or retry lost: %+v", poll.Session)
	}
	liveRequest(t, root, Request{Action: "live_select", Follow: true})
	poll = liveRequest(t, root, Request{Action: "live_poll"})
	if poll.Session == nil || poll.Session.ID != second {
		t.Fatal("did not follow producer")
	}
	liveRequest(t, root, Request{Action: "live_poll", ID: second, Revision: poll.Session.Revision, Offset: 2})
	view, err := c.Send(ctx, WireMessage{Op: "view"})
	if err != nil {
		t.Fatal(err)
	}
	if !view.ViewerActive || !view.Following || view.ViewedID != second || view.ViewedEvents != 2 {
		t.Fatalf("lost viewer ack: %+v", view)
	}
	c.Close()
	StopLive(root)
	opened := request(t, root, Request{Action: "open", ID: first}).(*Session)
	if opened.Status != "complete" || len(opened.Events) != 1 {
		t.Fatal("stream was not durably saved")
	}
	restarted := liveRequest(t, root, Request{Action: "server_start", Address: "127.0.0.1:0"})
	if !restarted.Listening {
		t.Fatal("could not restart")
	}
}
func TestLiveServerRefusesUnprotectedPublicListener(t *testing.T) {
	raw, _ := json.Marshal(Request{Action: "server_start", Address: "0.0.0.0:0"})
	if _, err := Execute(t.TempDir(), raw); err == nil {
		t.Fatal("public listener needs explicit token")
	}
}

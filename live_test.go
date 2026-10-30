// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"context"
	"encoding/json"
	"net"
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
	if _, err = c.Send(ctx, WireMessage{Op: "append", ID: first, Events: []Event{{"values": []float32{.1}}}}); err == nil {
		t.Fatal("TCP accepted neural samples")
	}
	// Re-open the same idempotent session to obtain its UDP routing secret.
	created, err := c.Send(ctx, WireMessage{Op: "create", Key: "run/case-one", Session: &Session{Title: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := c.TANHI(first, created.StreamToken)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if err = sender.SendEvent(Event{"Type": "forward", "layer_id": "grid/0/0/0/0", "values": []float32{.1, .2}}); err != nil {
		t.Fatal(err)
	}
	// Duplicate UDP datagrams must not duplicate recorded events.
	if _, err = sender.conn.Write(sender.packets[0]); err != nil {
		t.Fatal(err)
	}
	// Native Welvet TANHI traffic uses its own UDP socket and sequence space.
	addr, _ := net.ResolveUDPAddr("udp", sender.Address())
	native, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	sendNative := func(p TANHIPacket) {
		t.Helper()
		b, _ := json.Marshal(p)
		if _, e := native.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	sendNative(TANHIPacket{V: "tanhi1", Seq: 10, Phase: "sweep", Label: sender.StartMarker()})
	sendNative(TANHIPacket{V: "tanhi1", Seq: 11, Phase: "fwd", Layer: "Dense"})
	sendNative(TANHIPacket{V: "tanhi1", Seq: 12, Phase: "sweep", Label: sender.EndMarker()})
	receipt, err := sender.Flush(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Telemetry.Samples != 1 || receipt.Telemetry.Native != 1 || receipt.Telemetry.Partial {
		t.Fatalf("bad UDP receipt: %+v", receipt)
	}
	if _, err = c.Send(ctx, WireMessage{Op: "finish", ID: first, ExpectedPackets: 1, Status: "complete"}); err != nil {
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
	if poll.Session == nil || poll.Session.ID != first || len(poll.Session.Events) != 2 {
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
	if opened.Status != "complete" || len(opened.Events) != 2 {
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

func TestTANHIReordersSamplesAndReportsMissingPackets(t *testing.T) {
	h := &liveHub{stream: &Session{ID: "case", Status: "running"}, streamToken: "secret", sampleQueue: map[uint64]Event{}, nativeBindings: map[string]*nativeBinding{}}
	packet := func(seq uint64) TANHIPacket {
		return TANHIPacket{V: "tanhi1", Seq: seq, SessionID: "case", StreamToken: "secret", Event: Event{"Type": "forward"}}
	}
	h.acceptTANHILocked(packet(2), "samples")
	r := h.receipt(2)
	if len(r.Missing) != 1 || r.Missing[0] != 1 || !r.Telemetry.Partial {
		t.Fatalf("missing sample not reported: %+v", r)
	}
	h.acceptTANHILocked(packet(1), "samples")
	h.acceptTANHILocked(packet(2), "samples")
	if len(h.pending) != 2 || h.sampleThrough != 2 || h.receipt(2).Telemetry.Partial {
		t.Fatal("UDP reorder/dedup failed")
	}
	invalid := packet(3)
	invalid.StreamToken = "wrong"
	h.acceptTANHILocked(invalid, "samples")
	if h.udpStats.Rejected != 1 || h.sampleThrough != 2 {
		t.Fatal("accepted wrong stream secret")
	}
	h.acceptTANHILocked(TANHIPacket{V: "tanhi1", Seq: 10, Phase: "sweep", Label: "snat:case:secret"}, "native")
	h.acceptTANHILocked(TANHIPacket{V: "tanhi1", Seq: 12, Phase: "sweep", Label: "snat-end:case:secret"}, "native")
	if h.receipt(2).Telemetry.NativeMissing != 1 {
		t.Fatal("native packet gap not reported")
	}
}

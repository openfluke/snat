// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestChapterBoundariesRetryAndPersistence(t *testing.T) {
	root := t.TempDir()
	status := liveRequest(t, root, Request{Action: "server_start", Address: "127.0.0.1:0"})
	defer StopLive(root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, status.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	created, err := c.Send(ctx, WireMessage{Op: "create", Key: "group", Session: &Session{Title: "Number formats"}})
	if err != nil {
		t.Fatal(err)
	}
	begin := func(id string) WireReply {
		t.Helper()
		reply, e := c.Send(ctx, WireMessage{Op: "begin_chapter", ID: created.ID, Chapter: &Chapter{ID: id, Name: id, Layers: []Layer{{ID: "grid/0/0/0/0", Type: id}}}})
		if e != nil {
			t.Fatal(e)
		}
		return reply
	}
	send := func(reply WireReply) {
		t.Helper()
		sender, e := c.TANHI(created.ID, reply.StreamToken)
		if e != nil {
			t.Fatal(e)
		}
		defer sender.Close()
		if e = sender.SendEvent(Event{"Type": "forward", "values": []float32{1}}); e != nil {
			t.Fatal(e)
		}
		if _, e = sender.Flush(ctx, false); e != nil {
			t.Fatal(e)
		}
	}
	first := begin("float32")
	send(first)
	end := func(id string) {
		t.Helper()
		_, e := c.Send(ctx, WireMessage{Op: "end_chapter", ID: created.ID, Chapter: &Chapter{ID: id}, ExpectedPackets: 1, Status: "complete"})
		if e != nil {
			t.Fatal(e)
		}
	}
	end("float32")
	second := begin("float16")
	send(second)
	// An interrupted last chapter is retried from its event boundary, with a fresh
	// routing token. Earlier completed chapters and their geometry survive.
	retry := begin("float16")
	if retry.StreamToken == second.StreamToken {
		t.Fatal("retry reused routing token")
	}
	send(retry)
	end("float16")
	if _, err = c.Send(ctx, WireMessage{Op: "finish", ID: created.ID, Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Request{Action: "open", ID: created.ID})
	v, err := Execute(root, raw)
	if err != nil {
		t.Fatal(err)
	}
	saved := v.(*Session)
	if len(saved.Chapters) != 2 || len(saved.Events) != 2 {
		t.Fatalf("duplicate/lost chapters: %+v", saved)
	}
	for i, ch := range saved.Chapters {
		if ch.StartEvent != i || ch.EndEvent != i+1 || ch.Status != "complete" || saved.Events[i]["chapter_id"] != ch.ID || ch.Layers[0].Type != ch.ID {
			t.Fatalf("lost boundary or geometry: %+v", ch)
		}
	}
	again := begin("float32")
	if again.Chapter == nil || again.Chapter.Status != "complete" || again.EventCount != 2 {
		t.Fatal("completed retry was not idempotent")
	}
	StopLive(root)
	reopened := request(t, root, Request{Action: "open", ID: created.ID}).(*Session)
	if len(reopened.Chapters) != 2 || len(reopened.Events) != 2 {
		t.Fatal("chapters not durable")
	}
}

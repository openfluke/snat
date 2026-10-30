// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func (h *liveHub) currentChapter() *Chapter {
	if h.stream == nil || len(h.stream.Chapters) == 0 {
		return nil
	}
	return &h.stream.Chapters[len(h.stream.Chapters)-1]
}

// Both hub and disk mutexes are held by receive. Chapter boundaries are
// management metadata; neural tensors continue to arrive exclusively over UDP.
func (h *liveHub) manageChapterLocked(m WireMessage) WireReply {
	fail := func(err error) WireReply { return WireReply{Error: err.Error()} }
	if h.stream == nil || h.stream.ID != m.ID {
		return fail(fmt.Errorf("session is not the current producer stream"))
	}
	if m.Chapter == nil || m.Chapter.ID == "" {
		return fail(fmt.Errorf("chapter id required"))
	}
	if err := h.flushTelemetryLocked(); err != nil {
		return fail(err)
	}
	s := h.stream
	var state streamState
	if err := readJSON(statePath(h.root, s.ID), &state); err != nil {
		return fail(err)
	}
	if m.Op == "begin_chapter" {
		for i, existing := range state.Chapters {
			if existing.ID != m.Chapter.ID {
				continue
			}
			if existing.Status != "running" {
				reply := h.receipt(0)
				reply.Chapter = &existing
				reply.StreamToken = state.Token
				return reply
			}
			if i != len(state.Chapters)-1 {
				return fail(fmt.Errorf("only the last unfinished chapter can be resumed"))
			}
			// Retry the unfinished example atomically from its boundary. Old frame files
			// beyond the journal head are ignored and overwritten as the retry arrives.
			s.Events = s.Events[:existing.StartEvent]
			state.Frames = existing.StartFrame
			state.Events = existing.StartEvent
			state.Bytes = existing.StartBytes
			state.Chapters = state.Chapters[:i]
			break
		}
		if len(state.Chapters) > 0 && state.Chapters[len(state.Chapters)-1].Status == "running" {
			return fail(fmt.Errorf("finish the current chapter first"))
		}
		ch := *m.Chapter
		ch.StartEvent = len(s.Events)
		ch.EndEvent = ch.StartEvent
		ch.StartFrame = state.Frames
		ch.StartBytes = state.Bytes
		ch.Status = "running"
		state.Chapters = append(state.Chapters, ch)
		var secret [24]byte
		if _, err := rand.Read(secret[:]); err != nil {
			return fail(err)
		}
		state.Token = hex.EncodeToString(secret[:])
		state.Status = "running"
		state.Telemetry = nil
		state.Revision++
		if err := writeJSON(statePath(h.root, s.ID), state); err != nil {
			return fail(err)
		}
		applyStreamState(s, state)
		h.resetTelemetry(s, state.Token)
		// Sample sequence numbers are per chapter, not per grouped session.
		h.sampleThrough = 0
		h.udpStats.Samples = 0
		h.view.ProducerID = s.ID
		h.updated = time.Now().UTC()
		h.remember(s)
		reply := h.receipt(0)
		reply.StreamToken = state.Token
		reply.Chapter = &ch
		return reply
	}
	if len(state.Chapters) == 0 {
		return fail(fmt.Errorf("no active chapter"))
	}
	ch := &state.Chapters[len(state.Chapters)-1]
	if ch.ID != m.Chapter.ID {
		return fail(fmt.Errorf("chapter is not current"))
	}
	switch m.Status {
	case "complete", "unsupported", "failed", "cancelled":
	default:
		return fail(fmt.Errorf("invalid chapter status"))
	}
	receipt := h.receipt(m.ExpectedPackets)
	if !receipt.OK {
		return receipt
	}
	if m.ExpectedPackets > 0 && !receipt.Telemetry.NativeClosed {
		receipt.Telemetry.Partial = true
	}
	ch.Status = m.Status
	ch.EndEvent = len(s.Events)
	ch.Metadata = m.Chapter.Metadata
	if ch.Metadata == nil {
		ch.Metadata = map[string]any{}
	}
	ch.Metadata["telemetry"] = receipt.Telemetry
	state.Telemetry = &receipt.Telemetry
	state.Revision++
	if m.Progress != nil {
		state.Progress = m.Progress
		h.progress = *m.Progress
	}
	if err := writeJSON(statePath(h.root, s.ID), state); err != nil {
		return fail(err)
	}
	applyStreamState(s, state)
	h.remember(s)
	h.updated = time.Now().UTC()
	receipt.Chapter = &s.Chapters[len(s.Chapters)-1]
	receipt.Revision = s.Revision
	return receipt
}

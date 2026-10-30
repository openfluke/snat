// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.
package snat

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const WireProtocol = "snat/1"
const MaxWireFrame = 8 << 20

// Progress describes the producer's bounded catalogue and the current case.
type Progress struct {
	RunID       string  `json:"run_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Index       int64   `json:"index"`
	Total       int64   `json:"total"`
	Completed   int64   `json:"completed"`
	Succeeded   int64   `json:"succeeded"`
	Unsupported int64   `json:"unsupported"`
	Failed      int64   `json:"failed"`
	Percent     float64 `json:"percent"`
	Status      string  `json:"status"`
	Error       string  `json:"error,omitempty"`
}

// WireMessage is a newline-delimited JSON frame. Only trace writes are exposed;
// the TCP port cannot invoke filesystem imports, model loading, or exports.
type WireMessage struct {
	Protocol string    `json:"protocol"`
	Op       string    `json:"op"`
	Token    string    `json:"token,omitempty"`
	Key      string    `json:"key,omitempty"`
	ID       string    `json:"id,omitempty"`
	Sequence int64     `json:"sequence"`
	Session  *Session  `json:"session,omitempty"`
	Events   []Event   `json:"events,omitempty"`
	Progress *Progress `json:"progress,omitempty"`
	Status   string    `json:"status,omitempty"`
}
type WireReply struct {
	ViewedID     string `json:"viewed_id,omitempty"`
	ViewedEvents int    `json:"viewed_events"`
	Following    bool   `json:"following"`
	ViewerActive bool   `json:"viewer_active"`
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	ID           string `json:"id,omitempty"`
	NextSequence int64  `json:"next_sequence"`
	Revision     int64  `json:"revision"`
}
type LiveView struct {
	Follow     bool   `json:"follow"`
	PinnedID   string `json:"pinned_id"`
	ProducerID string `json:"producer_id"`
}
type LiveStatus struct {
	EventOffset int       `json:"event_offset"`
	Listening   bool      `json:"listening"`
	Address     string    `json:"address"`
	Connections int       `json:"connections"`
	Protocol    string    `json:"protocol"`
	Updated     time.Time `json:"updated"`
	View        LiveView  `json:"view"`
	Progress    Progress  `json:"progress"`
	Recent      []Summary `json:"recent"`
	Session     *Session  `json:"session,omitempty"`
}
type liveHub struct {
	stream         *Session
	viewedID       string
	viewedEvents   int
	polled         time.Time
	stopping       bool
	mu             sync.Mutex
	root           string
	listener       net.Listener
	address, token string
	clients        map[net.Conn]struct{}
	wg             sync.WaitGroup
	view           LiveView
	progress       Progress
	updated        time.Time
	recent         []Summary
}

var liveHubs = struct {
	sync.Mutex
	items map[string]*liveHub
}{items: map[string]*liveHub{}}

func hubFor(dataRoot string) *liveHub {
	root := filepath.Join(dataRoot, "neuralwave")
	liveHubs.Lock()
	defer liveHubs.Unlock()
	if h := liveHubs.items[root]; h != nil {
		return h
	}
	h := &liveHub{root: root, clients: map[net.Conn]struct{}{}, view: LiveView{Follow: true}, recent: []Summary{}}
	diskMu.Lock()
	_ = readJSON(filepath.Join(root, "live-view.json"), &h.view)
	diskMu.Unlock()
	liveHubs.items[root] = h
	return h
}
func isLiveAction(action string) bool {
	switch action {
	case "server_start", "server_stop", "live_status", "live_poll", "live_select":
		return true
	}
	return false
}
func executeLive(dataRoot string, r Request) (any, error) {
	if dataRoot == "" {
		return nil, fmt.Errorf("missing application data root")
	}
	h := hubFor(dataRoot)
	switch r.Action {
	case "server_start":
		if err := h.start(r.Address, r.Token); err != nil {
			return nil, err
		}
	case "server_stop":
		h.stop()
	case "live_select":
		h.mu.Lock()
		defer h.mu.Unlock()
		diskMu.Lock()
		defer diskMu.Unlock()
		if r.ID != "" {
			if _, err := loadSession(h.root, r.ID); err != nil {
				return nil, err
			}
		}
		view := h.view
		view.Follow = r.Follow
		view.PinnedID = r.ID
		if err := writeJSON(filepath.Join(h.root, "live-view.json"), view); err != nil {
			return nil, err
		}
		h.view = view
		return h.statusLocked(r), nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	diskMu.Lock()
	defer diskMu.Unlock()
	if r.Action == "live_poll" {
		h.polled = time.Now()
		h.viewedID = r.ID
		h.viewedEvents = r.Offset
	}
	return h.statusLocked(r), nil
}
func (h *liveHub) statusLocked(r Request) LiveStatus {
	result := LiveStatus{Listening: h.listener != nil, Address: h.address, Connections: len(h.clients), Protocol: WireProtocol, Updated: h.updated, View: h.view, Progress: h.progress, Recent: append([]Summary{}, h.recent...)}
	id := h.view.PinnedID
	if h.view.Follow {
		id = h.view.ProducerID
	}
	if r.Action == "live_poll" && id != "" && (r.IncludeSession == nil || *r.IncludeSession) {
		var session *Session
		var err error
		if h.stream != nil && h.stream.ID == id {
			session = h.stream
			var state streamState
			err = readJSON(statePath(h.root, id), &state)
			if err == nil {
				applyStreamState(session, state)
			}
		} else {
			session, err = loadSession(h.root, id)
		}
		if err == nil && (r.ID != id || r.Revision != session.Revision) {
			copy := *session
			copy.Metadata = map[string]any{}
			for key, value := range session.Metadata {
				copy.Metadata[key] = value
			}
			offset := 0
			if r.ID == id && r.Received >= 0 && r.Received <= len(session.Events) {
				offset = r.Received
				copy.Layers = nil
			}
			copy.Events = append([]Event{}, session.Events[offset:]...)
			result.EventOffset = offset
			result.Session = &copy
		}
	}

	return result
}
func (h *liveHub) start(address, token string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopping {
		return fmt.Errorf("SNAT TCP server is stopping")
	}
	if h.listener != nil {
		return fmt.Errorf("SNAT TCP server is already listening at %s", h.address)
	}
	if address == "" {
		address = "127.0.0.1:9731"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if token == "" && host != "localhost" && !(net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return fmt.Errorf("a token is required when listening beyond loopback")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	h.listener = listener
	h.address = listener.Addr().String()
	h.token = token
	h.wg.Add(1)
	go h.accept(listener)
	return nil
}
func (h *liveHub) accept(listener net.Listener) {
	defer h.wg.Done()
	for {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		h.mu.Lock()
		if h.listener != listener {
			h.mu.Unlock()
			c.Close()
			return
		}
		h.clients[c] = struct{}{}
		token := h.token
		h.wg.Add(1)
		h.mu.Unlock()
		go h.serve(c, token)
	}
}
func (h *liveHub) serve(c net.Conn, token string) {
	defer h.wg.Done()
	defer func() { c.Close(); h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }()
	scan := bufio.NewScanner(c)
	scan.Buffer(make([]byte, 64<<10), MaxWireFrame)
	encoder := json.NewEncoder(c)
	for {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Minute))
		if !scan.Scan() {
			return
		}
		var m WireMessage
		reply := WireReply{}
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			reply.Error = err.Error()
		} else if m.Protocol != WireProtocol {
			reply.Error = "expected protocol snat/1"
		} else if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(m.Token)) != 1 {
			reply.Error = "invalid token"
		} else {
			reply = h.receive(m)
			h.mu.Lock()
			reply.ViewedID = h.viewedID
			reply.ViewedEvents = h.viewedEvents
			reply.Following = h.view.Follow
			reply.ViewerActive = time.Since(h.polled) < 3*time.Second
			h.mu.Unlock()
		}
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := encoder.Encode(reply); err != nil {
			return
		}
	}
}
func (h *liveHub) receive(m WireMessage) WireReply {
	h.mu.Lock()
	defer h.mu.Unlock()
	diskMu.Lock()
	defer diskMu.Unlock()
	fail := func(err error) WireReply { return WireReply{Error: err.Error()} }
	switch m.Op {
	case "hello", "ping", "view":
		return WireReply{OK: true}
	case "progress":
		if m.Progress == nil {
			return fail(fmt.Errorf("missing progress"))
		}
		h.progress = *m.Progress
		h.updated = time.Now().UTC()
		return WireReply{OK: true}
	case "create":
		if m.Session == nil || strings.TrimSpace(m.Key) == "" {
			return fail(fmt.Errorf("create requires key and session"))
		}
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(m.Key)))[:32]
		s, err := loadSession(h.root, id)
		if errors.Is(err, os.ErrNotExist) {
			s = m.Session
			s.ID = id
			s.Created = time.Now().UTC()
			s.Schema = SchemaVersion
			s.Status = "running"
			s.Revision = 1
			s.NextSequence = 0
			s.Events = []Event{}
			err = createStream(h.root, s)
		}
		if err != nil {
			return fail(err)
		}
		h.stream = s
		h.view.ProducerID = id
		h.updated = time.Now().UTC()
		if m.Progress != nil {
			h.progress = *m.Progress
		}
		h.remember(s)
		return WireReply{OK: true, ID: id, NextSequence: s.NextSequence, Revision: s.Revision}
	case "append", "finish", "focus":
		var s *Session
		var err error
		if h.stream != nil && h.stream.ID == m.ID {
			s = h.stream
		} else {
			s, err = loadSession(h.root, m.ID)
		}
		if err != nil {
			return fail(err)
		}
		if m.Op == "focus" {
			h.view.ProducerID = s.ID
			h.updated = time.Now().UTC()
			return WireReply{OK: true, ID: s.ID, NextSequence: s.NextSequence, Revision: s.Revision}
		}
		if !s.Streamed {
			return fail(fmt.Errorf("TCP append requires a streaming session"))
		}
		var state streamState
		if err = readJSON(statePath(h.root, s.ID), &state); err != nil {
			return fail(err)
		}
		applyStreamState(s, state)
		if m.Sequence > state.Next {
			return fail(fmt.Errorf("sequence gap: expected %d", state.Next))
		}
		if m.Sequence < state.Next {
			return WireReply{OK: true, ID: s.ID, NextSequence: s.NextSequence, Revision: s.Revision}
		}
		if state.Status != "running" {
			return fail(fmt.Errorf("session is already %s", state.Status))
		}
		if m.Op == "append" {
			if len(m.Events) == 0 {
				return fail(fmt.Errorf("append needs events"))
			}
			encoded, e := json.Marshal(m.Events)
			if e != nil {
				return fail(e)
			}
			if state.Bytes+int64(len(encoded)) > maxDocument {
				return fail(fmt.Errorf("recording exceeds 64 MiB; reduce samples"))
			}
			if err = writeJSON(framePath(h.root, s.ID, state.Frames), m.Events); err != nil {
				return fail(err)
			}
			state.Bytes += int64(len(encoded))
			state.Events += len(m.Events)
			state.Frames++
		} else {
			switch m.Status {
			case "complete", "unsupported", "failed", "cancelled":
				state.Status = m.Status
			default:
				return fail(fmt.Errorf("invalid final status"))
			}
		}
		state.Next++
		state.Revision++
		if m.Progress != nil {
			state.Progress = m.Progress
		}
		if err = writeJSON(statePath(h.root, s.ID), state); err != nil {
			return fail(err)
		}
		if m.Op == "append" {
			s.Events = append(s.Events, m.Events...)
		}
		applyStreamState(s, state)
		h.stream = s
		if m.Op == "finish" {
			if err = rememberSession(h.root, s); err != nil {
				return fail(err)
			}
		}
		h.updated = time.Now().UTC()
		if m.Progress != nil {
			h.progress = *m.Progress
		}
		h.remember(s)
		return WireReply{OK: true, ID: s.ID, NextSequence: s.NextSequence, Revision: s.Revision}

	default:
		return fail(fmt.Errorf("unknown wire op %q", m.Op))
	}
}
func summaryOf(s *Session) Summary {
	return Summary{ID: s.ID, Title: s.Title, Model: s.Model, Created: s.Created, Source: s.Source, Layers: len(s.Layers), Events: max(len(s.Events), s.EventCount), Status: s.Status}
}
func (h *liveHub) remember(s *Session) {
	next := []Summary{summaryOf(s)}
	for _, old := range h.recent {
		if old.ID != s.ID && len(next) < 100 {
			next = append(next, old)
		}
	}
	h.recent = next
}
func (h *liveHub) stop() {
	h.mu.Lock()
	listener := h.listener
	h.stopping = true
	h.listener = nil
	if listener != nil {
		listener.Close()
	}
	for c := range h.clients {
		c.Close()
	}
	h.mu.Unlock()
	h.wg.Wait()
	h.mu.Lock()
	h.stopping = false
	h.mu.Unlock()
}

// StopLive releases listeners and connections owned by an application data root.
func StopLive(dataRoot string) {
	liveHubs.Lock()
	h := liveHubs.items[filepath.Join(dataRoot, "neuralwave")]
	liveHubs.Unlock()
	if h != nil {
		h.stop()
	}
}

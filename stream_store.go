// SPDX-License-Identifier: Apache-2.0
package snat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type streamState struct {
	Status   string    `json:"status"`
	Title    string    `json:"title"`
	Next     int64     `json:"next"`
	Revision int64     `json:"revision"`
	Frames   int64     `json:"frames"`
	Events   int       `json:"events"`
	Bytes    int64     `json:"bytes"`
	Progress *Progress `json:"progress,omitempty"`
}

func statePath(root, id string) string { return filepath.Join(root, "sessions", id, "stream.json") }
func framePath(root, id string, seq int64) string {
	return filepath.Join(root, "sessions", id, "frames", fmt.Sprintf("%020d.json", seq))
}
func applyStreamState(s *Session, state streamState) {
	s.Status = state.Status
	s.Title = state.Title
	s.Revision = state.Revision
	s.NextSequence = state.Next
	s.EventCount = state.Events
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	if state.Progress != nil {
		s.Progress = state.Progress.Percent
		s.Metadata["progress"] = state.Progress
	}
}
func createStream(root string, s *Session) error {
	s.Streamed = true
	s.Events = []Event{}
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	path, err := sessionPath(root, s.ID)
	if err != nil {
		return err
	}
	if err = writeJSON(path, s); err != nil {
		return err
	}
	state := streamState{Status: "running", Title: s.Title, Revision: 1}
	if err = writeJSON(statePath(root, s.ID), state); err != nil {
		return err
	}
	applyStreamState(s, state)
	return rememberSession(root, s)
}
func loadStream(root string, s *Session) error {
	var state streamState
	if err := readJSON(statePath(root, s.ID), &state); err != nil {
		return err
	}
	applyStreamState(s, state)
	for i := int64(0); i < state.Frames; i++ {
		var events []Event
		if err := readJSON(framePath(root, s.ID, i), &events); err != nil {
			return err
		}
		s.Events = append(s.Events, events...)
	}
	if len(s.Events) != state.Events {
		return fmt.Errorf("stream event count mismatch")
	}
	return nil
}
func renameStream(root, id, title string) (*Session, error) {
	var state streamState
	if err := readJSON(statePath(root, id), &state); err != nil {
		return nil, err
	}
	state.Title = title
	state.Revision++
	if err := writeJSON(statePath(root, id), state); err != nil {
		return nil, err
	}
	s, err := loadSession(root, id)
	if err == nil {
		err = rememberSession(root, s)
	}
	return s, err
}
func rememberSession(root string, s *Session) error {
	recent := []Summary{}
	err := readJSON(filepath.Join(root, "recent-sessions.json"), &recent)
	if os.IsNotExist(err) {
		recent, err = scanSessions(root)
	}
	if err != nil {
		return err
	}
	next := []Summary{summaryOf(s)}
	for _, entry := range recent {
		if entry.ID != s.ID {
			next = append(next, entry)
		}
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].Created.After(next[j].Created) })
	if len(next) > 200 {
		next = next[:200]
	}
	return writeJSON(filepath.Join(root, "recent-sessions.json"), next)
}

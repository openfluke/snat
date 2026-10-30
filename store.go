// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var diskMu sync.Mutex

const maxDocument = 64 << 20

func readJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxDocument+1))
	if err != nil {
		return err
	}
	if len(raw) > maxDocument {
		return fmt.Errorf("document exceeds 64 MiB: %s", path)
	}
	return json.Unmarshal(raw, value)
}
func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > maxDocument {
		return fmt.Errorf("document exceeds 64 MiB")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".snat-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func sessionPath(root, id string) (string, error) {
	if len(id) != 32 {
		return "", fmt.Errorf("invalid session ID")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", fmt.Errorf("invalid session ID")
	}
	return filepath.Join(root, "sessions", id, "session.json"), nil
}
func saveSession(root string, s *Session) (*Session, error) {
	if s == nil {
		return nil, fmt.Errorf("missing session")
	}
	if s.ID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		s.ID = hex.EncodeToString(id[:])
		s.Created = time.Now().UTC()
	}
	if s.Schema != 0 && s.Schema != SchemaVersion {
		return nil, fmt.Errorf("unsupported session schema %d", s.Schema)
	}
	s.Schema = SchemaVersion
	if strings.TrimSpace(s.Title) == "" {
		s.Title = s.Model
	}
	if s.Title == "" {
		s.Title = "Untitled neural session"
	}
	if s.Layers == nil {
		s.Layers = []Layer{}
	}
	if s.Events == nil {
		s.Events = []Event{}
	}
	path, err := sessionPath(root, s.ID)
	if err != nil {
		return nil, err
	}
	if err = writeJSON(path, s); err != nil {
		return nil, err
	}
	return s, nil
}
func loadSession(root, id string) (*Session, error) {
	path, err := sessionPath(root, id)
	if err != nil {
		return nil, err
	}
	var s Session
	if err = readJSON(path, &s); err != nil {
		return nil, err
	}
	if s.Schema != SchemaVersion {
		return nil, fmt.Errorf("unsupported session schema %d", s.Schema)
	}
	return &s, nil
}
func listSessions(root string) ([]Summary, error) {
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if os.IsNotExist(err) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Summary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := loadSession(root, e.Name())
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", e.Name(), err)
		}
		result = append(result, Summary{s.ID, s.Title, s.Model, s.Created, s.Source, len(s.Layers), len(s.Events)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}
func loadModels(root string) ([]Model, error) {
	models := []Model{}
	err := readJSON(filepath.Join(root, "models.json"), &models)
	if os.IsNotExist(err) {
		err = nil
	}
	for i := range models {
		if models[i].ID == "" {
			models[i].ID = modelID(models[i].Path)
		}
	}
	return models, err
}

// Execute serializes library mutations independently of the game simulation.
func Execute(dataRoot string, request []byte) (result any, err error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			result = nil
			err = fmt.Errorf("SNAT: %v", p)
		}
	}()
	if dataRoot == "" {
		return nil, fmt.Errorf("missing application data root")
	}
	root := filepath.Join(dataRoot, "neuralwave")
	var r Request
	if err = json.Unmarshal(request, &r); err != nil {
		return nil, err
	}
	switch r.Action {
	case "library":
		sessions, e := listSessions(root)
		if e != nil {
			return nil, e
		}
		models, e := loadModels(root)
		return map[string]any{"version": Version, "root": root, "sessions": sessions, "models": models}, e
	case "create":
		return saveSession(root, &Session{Title: r.Title, Source: "recording", Layers: []Layer{}, Events: []Event{}})
	case "append_log":
		s, e := loadSession(root, r.ID)
		if e != nil {
			return nil, e
		}
		var log struct {
			Layers []Layer `json:"layers"`
			Events []Event `json:"events"`
		}
		if e = readJSON(r.Path, &log); e != nil {
			return nil, e
		}
		if log.Events == nil {
			return nil, fmt.Errorf("log needs an events array")
		}
		if len(s.Layers) == 0 {
			s.Layers = log.Layers
		}
		if len(s.Layers) == 0 {
			return nil, fmt.Errorf("a new recording needs layers alongside events")
		}
		s.Events = append(s.Events, log.Events...)
		if s.Source == "recording" {
			s.Description = "Recorded events appended from JSON logs. Event payloads are preserved verbatim."
		}
		return saveSession(root, s)
	case "save":
		return saveSession(root, r.Session)
	case "open":
		return loadSession(root, r.ID)
	case "rename":
		s, e := loadSession(root, r.ID)
		if e != nil {
			return nil, e
		}
		s.Title = r.Title
		return saveSession(root, s)
	case "export":
		s, e := loadSession(root, r.ID)
		if e != nil {
			return nil, e
		}
		if !filepath.IsAbs(r.Path) {
			return nil, fmt.Errorf("export needs an absolute file path")
		}
		// Explicit exports never replace an existing file.
		f, e := os.OpenFile(r.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		e = json.NewEncoder(f).Encode(s)
		ce := f.Close()
		if e != nil {
			os.Remove(r.Path)
			return nil, e
		}
		return map[string]any{"path": r.Path}, ce
	case "import":
		return importFolder(root, r.Path)
	case "import_session":
		var s Session
		if e := readJSON(r.Path, &s); e != nil {
			return nil, e
		}
		s.ID = ""
		return saveSession(root, &s)
	case "snapshot":
		models, e := loadModels(root)
		if e != nil {
			return nil, e
		}
		for _, m := range models {
			if (r.ModelID != "" && m.ID == r.ModelID) || (r.ModelID == "" && m.Name == r.Name) {
				if m.Analysis == nil {
					return nil, fmt.Errorf("analyse the model first")
				}
				return saveSession(root, &Session{Title: r.Title, Model: m.Name, Source: "weight-analysis", Layers: m.Analysis.Layers, Analysis: m.Analysis, BlankAnalysis: m.BlankAnalysis, Description: "Measured weight statistics and sampled weights. This is a static model view, not an activity recording."})
			}
		}
		return nil, fmt.Errorf("model not found")
	case "scan", "select", "verify", "analyze", "compare", "inspect", "benchmark":
		return modelAction(root, r)
	default:
		return nil, fmt.Errorf("unknown SNAT action %q", r.Action)
	}
}

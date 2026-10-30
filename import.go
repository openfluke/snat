// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func legacyLayers(raw []json.RawMessage) ([]Layer, error) {
	layers := make([]Layer, 0, len(raw))
	for i, b := range raw {
		var l Layer
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		var legacy struct {
			Parameters int64             `json:"parameters"`
			Branches   []json.RawMessage `json:"branches"`
		}
		if err := json.Unmarshal(b, &legacy); err != nil {
			return nil, err
		}
		if l.Params == 0 {
			l.Params = legacy.Parameters
		}
		if l.Name == "" {
			l.Name = fmt.Sprintf("%s [%d,%d,%d] #%d", l.Type, l.GridRow, l.GridCol, l.CellLayer, i)
		}
		if len(legacy.Branches) > 0 {
			var err error
			l.Branches, err = legacyLayers(legacy.Branches)
			if err != nil {
				return nil, err
			}
		}
		layers = append(layers, l)
	}
	return layers, nil
}
func activityPrefix(id string) string {
	aliases := map[string]string{"network1_all_types": "model1", "network2_grid_scatter": "model2", "network3_normal_mode": "model3", "network3_step_mode": "model3_stepping", "network4_grid_demo": "model4", "network5_parallel_demo": "model5"}
	if v := aliases[id]; v != "" {
		return v
	}
	return strings.ReplaceAll(id, "_", "")
}

// importFolder snapshots files into Fable. Replays remain usable after the source moves.
func importFolder(root, path string) ([]Summary, error) {
	if path == "" {
		return nil, fmt.Errorf("choose a telemetry folder")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	pending := []*Session{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_telemetry.json") {
			continue
		}
		var t struct {
			ID     string            `json:"id"`
			Layers []json.RawMessage `json:"layers"`
		}
		if err = readJSON(filepath.Join(path, entry.Name()), &t); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if len(t.Layers) == 0 {
			return nil, fmt.Errorf("%s: no telemetry layers", entry.Name())
		}
		layers, e := legacyLayers(t.Layers)
		if e != nil {
			return nil, e
		}
		base := strings.TrimSuffix(entry.Name(), "_telemetry.json")
		id := t.ID
		if id == "" {
			id = base
		}
		s := &Session{Title: id, Model: id, Source: "loom-import", Layers: layers, Events: []Event{}, Description: "Imported LOOM/Welvet telemetry. Geometry represents layer metadata; event payloads are preserved verbatim."}
		// Prefer the same basename, then the legacy prototype's model-ID aliases.
		candidates := []string{base + "_activity.json", activityPrefix(id) + "_activity.json"}
		for _, candidate := range candidates {
			ap := filepath.Join(path, candidate)
			if _, e = os.Stat(ap); os.IsNotExist(e) {
				continue
			} else if e != nil {
				return nil, e
			}
			var a struct {
				Events []Event `json:"events"`
			}
			if e = readJSON(ap, &a); e != nil {
				return nil, fmt.Errorf("%s: %w", candidate, e)
			}
			if a.Events == nil {
				return nil, fmt.Errorf("%s: missing events array", candidate)
			}
			s.Events = a.Events
			break
		}
		pending = append(pending, s)
	}
	if len(pending) == 0 {
		return nil, fmt.Errorf("no *_telemetry.json files in %s", path)
	}
	result := []Summary{}
	for _, s := range pending {
		if _, err = saveSession(root, s); err != nil {
			return nil, err
		}
		result = append(result, Summary{s.ID, s.Title, s.Model, s.Created, s.Source, len(s.Layers), len(s.Events)})
	}
	return result, nil
}

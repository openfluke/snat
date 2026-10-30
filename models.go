// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func modelID(path string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(path))))[:24]
}

func discover(path string) ([]Model, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".cache", "huggingface", "hub")
	}
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	models := []Model{}
	// WalkDir does not descend symlinked cache blobs; snapshot tensor symlinks are
	// opened by the analyser. Limit directory nesting and ignore blob storage.
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(path, p)
		if d.IsDir() {
			if d.Name() == "blobs" || strings.Count(rel, string(filepath.Separator)) > 7 {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "config.json" || strings.HasSuffix(d.Name(), ".entity") {
			modelPath := p
			if d.Name() == "config.json" {
				modelPath = filepath.Dir(p)
				files, _ := filepath.Glob(filepath.Join(modelPath, "*.safetensors"))
				if len(files) == 0 {
					return nil
				}
			}
			name := rel
			if d.Name() == "config.json" {
				name = strings.TrimSuffix(rel, string(filepath.Separator)+"config.json")
				if name == "config.json" {
					name = filepath.Base(modelPath)
				}
			}
			models = append(models, Model{ID: modelID(modelPath), Name: name, Path: modelPath})
		}
		return nil
	})
	return models, err
}
func modelAction(root string, r Request) (any, error) {
	models, err := loadModels(root)
	if err != nil {
		return nil, err
	}
	if r.Action == "scan" {
		found, e := discover(r.Path)
		if e != nil {
			return nil, e
		}
		for _, m := range found {
			exists := false
			for _, old := range models {
				if old.Path == m.Path {
					exists = true
					break
				}
			}
			if !exists {
				models = append(models, m)
			}
		}
		sort.Slice(models, func(i, j int) bool { return naturalLess(models[i].Name, models[j].Name) })
	} else {
		matched := 0
		for i := range models {
			m := &models[i]
			if r.ModelID != "" {
				if m.ID != r.ModelID {
					continue
				}
			} else if r.Name != "" {
				if m.Name != r.Name {
					continue
				}
			} else if !m.Selected {
				continue
			}
			matched++
			if r.Action == "select" {
				m.Selected = !m.Selected
				continue
			}
			m.Error = ""
			switch r.Action {
			case "inspect":
				m.Inspection, err = inspectModel(m.Path)
			case "analyze", "compare":
				m.Analysis, err = analyzeModel(m.Path, true)
				if err == nil {
					m.BlankAnalysis = simulateBlankAnalysis(m.Analysis)
				}
			case "verify":
				_, err = withModel(root, m.Path, func(path string) error { _, e := loadTransformer(path); return e })
				m.Checked = true
				m.Loadable = err == nil
				m.LastChecked = time.Now().UTC()
			case "benchmark":
				var s *Session
				m.Benchmarked = false
				s, m.TPS, m.GeneratedTokens, err = benchmark(root, *m, r)
				if err == nil {
					m.ExampleOutput = s.Output
					_, err = saveSession(root, s)
					m.Benchmarked = err == nil
				}
			}
			if err != nil {
				m.Error = err.Error()
				err = nil
			}
		}
		if matched == 0 {
			return nil, fmt.Errorf("select at least one model")
		}
	}
	if err = writeJSON(filepath.Join(root, "models.json"), models); err != nil {
		return nil, err
	}
	return models, nil
}

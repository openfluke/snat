// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openfluke/welvet/model/entity"
	"github.com/openfluke/welvet/model/tokenizer"
	"github.com/openfluke/welvet/model/transformer"
)

func loadTransformer(path string) (*transformer.Model, error) { return transformer.LoadEntity(path) }

// Temporary conversion uses today's Welvet loader and leaves source models untouched.
func withModel(root, path string, fn func(string) error) (string, error) {
	if strings.HasSuffix(path, ".entity") {
		return path, fn(path)
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(path)))
	dir := filepath.Join(root, "cache", key)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	f, e := os.CreateTemp(dir, "convert-*.entity")
	if e != nil {
		return "", e
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	if e = entity.PackFromHF(path, tmp, entity.PackOptions{}); e != nil {
		return "", e
	}
	return tmp, fn(tmp)
}
func benchmark(root string, model Model, r Request) (*Session, float64, []uint32, error) {
	s := &Session{Title: model.Name + " · inference", Model: model.Name, Source: "welvet-1.1.3", Prompt: r.Prompt, Description: "CPU greedy inference. Events contain real output logits, sampled to 256 values. They do not represent internal layer activations.", Layers: []Layer{{Name: "Output logits", Type: "LMHead"}}, Events: []Event{}}
	if s.Prompt == "" {
		s.Prompt = "Hello, artificial"
	}
	budget := r.MaxTokens
	if budget == 0 {
		budget = 10
	}
	if budget < 1 || budget > 50 {
		return nil, 0, nil, fmt.Errorf("choose 1–50 generated tokens")
	}
	var tokens []uint32
	var tps float64
	_, err := withModel(root, model.Path, func(path string) error {
		m, e := loadTransformer(path)
		if e != nil {
			return e
		}
		tk, e := tokenizer.LoadForEntity(path, m.TokenizerPath, m.Snapshot, m.Repo)
		if e != nil {
			return e
		}
		ids := tk.Encode(s.Prompt, false)
		if len(ids) == 0 {
			return fmt.Errorf("empty tokenized prompt")
		}
		if len(ids) >= m.MaxSeqLen {
			return fmt.Errorf("prompt + generation exceed model context (%d)", m.MaxSeqLen)
		}
		budget = min(budget, m.MaxSeqLen-len(ids))
		s.Layers[0].Shape = []int{m.VocabSize}
		s.Layers[0].Params = int64(m.VocabSize)
		start := time.Now()
		m.ResetKV()
		for step := 0; step < budget; step++ {
			logits, e := m.ForwardTokens(ids)
			if e != nil {
				return e
			}
			if len(logits) == 0 {
				return fmt.Errorf("empty logits")
			}
			next := 0
			for i, v := range logits {
				if v > logits[next] {
					next = i
				}
			}
			sample := []float32{}
			for i := 0; i < len(logits); i += max(1, (len(logits)+255)/256) {
				sample = append(sample, logits[i])
			}
			s.Events = append(s.Events, Event{"Type": "forward", "mode": "normal", "grid_row": 0, "grid_col": 0, "cell_layer": 0, "step_count": step, "elapsed_ms": float64(time.Since(start).Microseconds()) / 1000, "token_id": next, "values": sample, "Stats": map[string]any{"LayerType": "LMHead"}})
			tokens = append(tokens, uint32(next))
			ids = []uint32{uint32(next)}
			stop := false
			for _, eos := range m.EOSTokens {
				if next == eos {
					stop = true
					break
				}
			}
			if stop {
				break
			}
		}
		tps = float64(len(tokens)) / time.Since(start).Seconds()
		s.Output = tk.Decode(tokens, true)
		return nil
	})
	return s, tps, tokens, err
}

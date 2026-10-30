// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"github.com/openfluke/welvet/model/entity"
	"os"
	"path/filepath"
)

func inspectModel(path string) (map[string]any, error) {
	if filepath.Ext(path) == ".entity" {
		f, err := entity.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return map[string]any{"entity_header": f.Header()}, nil
	}
	a, err := analyzeModel(path, false)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	configErr := readJSON(filepath.Join(path, "config.json"), &config)
	_, tokErr := os.Stat(filepath.Join(path, "tokenizer.json"))
	tensors := []TensorInfo{}
	recognized := 0
	for _, l := range a.Layers {
		t := classifyTensor(l.Name)
		tensors = append(tensors, t)
		if t.Recognized {
			recognized++
		}
	}
	return map[string]any{"config": config, "tensors": tensors, "analysis": a, "summary": map[string]any{
		"total_tensors": len(tensors), "recognized_tensors": recognized, "unknown_tensors": len(tensors) - recognized,
		"has_config": configErr == nil, "has_safetensors": true, "has_tokenizer": tokErr == nil,
	}}, nil
}

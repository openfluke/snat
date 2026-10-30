// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/openfluke/welvet/model/hf"
)

type tensorHeader struct {
	DType   string  `json:"dtype"`
	Shape   []int   `json:"shape"`
	Offsets []int64 `json:"data_offsets"`
}

func tensorHeaders(path string) (map[string]tensorHeader, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var n uint64
	if e = binary.Read(f, binary.LittleEndian, &n); e != nil {
		return nil, e
	}
	if n > 16<<20 {
		return nil, fmt.Errorf("tensor header exceeds 16 MiB")
	}
	b := make([]byte, n)
	if _, e = io.ReadFull(f, b); e != nil {
		return nil, e
	}
	raw := map[string]json.RawMessage{}
	if e = json.Unmarshal(b, &raw); e != nil {
		return nil, e
	}
	stat, e := f.Stat()
	if e != nil {
		return nil, e
	}
	result := map[string]tensorHeader{}
	for name, v := range raw {
		if name == "__metadata__" {
			continue
		}
		var h tensorHeader
		if e = json.Unmarshal(v, &h); e != nil {
			return nil, e
		}
		if len(h.Offsets) != 2 || h.Offsets[0] < 0 || h.Offsets[1] < h.Offsets[0] || h.Offsets[1] > stat.Size()-8-int64(n) {
			return nil, fmt.Errorf("%s: invalid data offsets", name)
		}
		count := int64(1)
		for _, d := range h.Shape {
			if d < 0 || d > 1<<30 || count > math.MaxInt64/int64(max(1, d)) {
				return nil, fmt.Errorf("%s: invalid shape", name)
			}
			count *= int64(d)
		}
		widths := map[string]int64{"F32": 4, "F16": 2, "BF16": 2, "F64": 8, "I64": 8, "I32": 4, "I16": 2, "I8": 1, "U8": 1, "BOOL": 1}
		width, ok := widths[h.DType]
		if !ok {
			return nil, fmt.Errorf("%s: unsupported dtype %s", name, h.DType)
		}
		if count > math.MaxInt64/width || count*width != h.Offsets[1]-h.Offsets[0] {
			return nil, fmt.Errorf("%s: shape/data size mismatch", name)
		}
		result[name] = h
	}
	return result, nil
}
func analyzeModel(path string, deep bool) (*Analysis, error) {
	files, e := filepath.Glob(filepath.Join(path, "*.safetensors"))
	if e != nil {
		return nil, e
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("weight analysis needs an HF SafeTensors directory; scan its source snapshot for ENTITY weights")
	}
	a := &Analysis{Layers: []Layer{}, Statistics: deep}
	var config map[string]any
	_ = readJSON(filepath.Join(path, "config.json"), &config)
	heads := func(key string) int { v, _ := config[key].(float64); return int(v) }
	seen := map[string]bool{}
	for _, file := range files {
		headers, e := tensorHeaders(file)
		if e != nil {
			return nil, e
		}
		for name, h := range headers {
			if seen[name] {
				return nil, fmt.Errorf("duplicate tensor %s across shards", name)
			}
			seen[name] = true
			count := int64(1)
			for _, d := range h.Shape {
				count *= int64(d)
			}
			l := Layer{Name: name, Type: classifyTensor(name).LayerType, Shape: h.Shape, Params: count, CellLayer: len(a.Layers)}
			if l.Type == "Attention.Q" || l.Type == "Attention.O" || l.Type == "Attention.QKV" {
				l.Heads = heads("num_attention_heads")
			}
			if l.Type == "Attention.K" || l.Type == "Attention.V" {
				l.Heads = heads("num_key_value_heads")
				if l.Heads == 0 {
					l.Heads = heads("num_attention_heads")
				}
			}
			if deep && count > 0 {
				// Decode one tensor at a time, with a bounded allocation budget.
				if count > 128<<20 {
					return nil, fmt.Errorf("%s exceeds the 512 MiB decoded tensor budget; inspect metadata instead", name)
				}
				ts, e := hf.LoadSafetensorsSelective(file, func(n string) bool { return n == name })
				if e != nil {
					return nil, e
				}
				data := ts[name]
				for _, v := range data {
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						return nil, fmt.Errorf("%s contains non-finite weights", name)
					}
				}
				l.Mean, l.StdDev, l.Min, l.Max = calcStats(data)
				for i := 0; i < len(data); i += max(1, (len(data)+255)/256) {
					l.Samples = append(l.Samples, data[i])
				}
			}
			a.TotalParams += count
			a.Layers = append(a.Layers, l)
		}
	}
	sortLayers(a.Layers)
	for i := range a.Layers {
		a.Layers[i].CellLayer = i
	}
	return a, nil
}

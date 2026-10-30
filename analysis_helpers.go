// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

// Classification and initialization estimates adapted from OpenFluke NeuralWave
// main.go/web.go prototype (Apache-2.0). Runtime analysis uses Welvet 1.1.3.
package snat

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type TensorInfo struct {
	Name       string `json:"name"`
	Recognized bool   `json:"recognized"`
	LayerType  string `json:"layer_type"`
	Pattern    string `json:"pattern,omitempty"` // Which pattern matched
}

func classifyTensor(name string) TensorInfo {
	lower := strings.ToLower(name)

	// Known patterns for LOOM-supported architectures
	patterns := []struct {
		pattern   string
		layerType string
	}{
		// Embeddings
		{"embed_tokens.weight", "Embedding"},
		{"wte.weight", "Embedding"},
		{"word_embeddings.weight", "Embedding"},
		{"embeddings.weight", "Embedding"},

		// Attention
		{"self_attn.q_proj", "Attention.Q"},
		{"self_attn.k_proj", "Attention.K"},
		{"self_attn.v_proj", "Attention.V"},
		{"self_attn.o_proj", "Attention.O"},
		{"attention.query", "Attention.Q"},
		{"attention.key", "Attention.K"},
		{"attention.value", "Attention.V"},
		{"attention.output", "Attention.O"},
		{"attn.c_attn", "Attention.QKV"},
		{"attn.c_proj", "Attention.O"},
		{"attn.q_proj", "Attention.Q"},
		{"attn.k_proj", "Attention.K"},
		{"attn.v_proj", "Attention.V"},
		{"attn.o_proj", "Attention.O"},

		// MLP/FFN
		{"mlp.gate_proj", "MLP.Gate"},
		{"mlp.up_proj", "MLP.Up"},
		{"mlp.down_proj", "MLP.Down"},
		{"mlp.c_fc", "MLP.FC"},
		{"mlp.c_proj", "MLP.Proj"},
		{"feed_forward", "MLP"},
		{"ffn", "MLP"},

		// Normalization
		{"input_layernorm", "Norm.Input"},
		{"post_attention_layernorm", "Norm.PostAttn"},
		{"ln_1", "Norm.Pre"},
		{"ln_2", "Norm.Post"},
		{"ln_f", "Norm.Final"},
		{"norm.weight", "Norm"},
		{"layernorm", "Norm"},
		{"rms_norm", "RMSNorm"},

		// Output
		{"lm_head", "LMHead"},

		// Rotary embeddings
		{"rotary_emb", "RotaryEmb"},
		{"inv_freq", "RotaryEmb"},
	}

	for _, p := range patterns {
		if strings.Contains(lower, p.pattern) {
			return TensorInfo{
				Name:       name,
				Recognized: true,
				LayerType:  p.layerType,
				Pattern:    p.pattern,
			}
		}
	}

	// Unknown tensor
	return TensorInfo{
		Name:       name,
		Recognized: false,
		LayerType:  "Unknown",
		Pattern:    "",
	}
}

func simulateBlankAnalysis(real *Analysis) *Analysis {
	blank := &Analysis{
		Statistics:  true,
		TotalParams: real.TotalParams,
		Layers:      make([]Layer, len(real.Layers)),
	}

	for i, l := range real.Layers {
		// Default: Xavier Init (Gaussian with mean 0, std = sqrt(2 / (fan_in + fan_out)))
		// Since we don't have exact dimensions, we approximate fan_in/out using Params.
		// Approximating square matrix: Dim ~ Sqrt(Params)
		dim := float64(math.Sqrt(float64(l.Params)))
		if dim < 1 {
			dim = 1
		}

		var std float64
		if strings.Contains(strings.ToLower(l.Type), "norm") {
			// Norm layers usually init to 1.0 (gamma) or 0.0 (beta)
			// Let's assume gamma (weights) = 1.0
			mean := float32(1)
			if strings.HasSuffix(l.Name, ".bias") {
				mean = 0
			}
			blank.Layers[i] = Layer{
				Name:  l.Name,
				Type:  l.Type,
				Heads: l.Heads, GridRow: l.GridRow, GridCol: l.GridCol, CellLayer: l.CellLayer,
				Params: l.Params,
				Shape:  l.Shape,
				Mean:   mean,
				StdDev: 0.0,
				Min:    mean,
				Max:    mean,
			}
			continue
		} else if l.Type == "Embedding" {
			// Embedding usually N(0, 1) or small uniform
			std = 1.0
		} else {
			// Linear/MLP/Attn: He/Kaiming initialization = sqrt(2/fan_in)
			std = math.Sqrt(2.0 / dim)
		}

		// Projected stats for Gaussian(0, std)
		blank.Layers[i] = Layer{
			Name:  l.Name,
			Type:  l.Type,
			Heads: l.Heads, GridRow: l.GridRow, GridCol: l.GridCol, CellLayer: l.CellLayer,
			Params: l.Params,
			Shape:  l.Shape, // Copied
			Mean:   0.0,
			StdDev: float32(std),
			Min:    float32(-3 * std),
			Max:    float32(3 * std),
		}
	}
	return blank
}

var naturalParts = regexp.MustCompile(`\d+|\D+`)

func naturalLess(s1, s2 string) bool {
	re := naturalParts
	parts1 := re.FindAllString(s1, -1)
	parts2 := re.FindAllString(s2, -1)

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		p1 := parts1[i]
		p2 := parts2[i]

		// Check if both are numbers
		n1, err1 := strconv.Atoi(p1)
		n2, err2 := strconv.Atoi(p2)

		if err1 == nil && err2 == nil {
			if n1 != n2 {
				return n1 < n2
			}
		} else {
			if p1 != p2 {
				return p1 < p2
			}
		}
	}
	return len(parts1) < len(parts2)
}

func calcStats(data []float32) (float32, float32, float32, float32) {
	if len(data) == 0 {
		return 0, 0, 0, 0
	}
	low, high := data[0], data[0]
	var mean, m2 float64
	for i, v := range data {
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
		delta := float64(v) - mean
		mean += delta / float64(i+1)
		m2 += delta * (float64(v) - mean)
	}
	return float32(mean), float32(math.Sqrt(m2 / float64(len(data)))), low, high
}

func sortLayers(layers []Layer) {
	sort.Slice(layers, func(i, j int) bool { return naturalLess(layers[i].Name, layers[j].Name) })
}

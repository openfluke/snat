// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

// Package snat records, stores, and inspects spatiotemporal neural activity traces.
// Sessions are portable JSON; consuming applications provide rendering and transport.
package snat

import "time"

const Version = "1.1.0"
const SchemaVersion = 1

type Layer struct {
	DType       string    `json:"dtype,omitempty"`
	ID          string    `json:"id,omitempty"`
	GridDepth   int       `json:"grid_depth,omitempty"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Heads       int       `json:"num_heads,omitempty"`
	Params      int64     `json:"params"`
	Shape       []int     `json:"shape"`
	Mean        float32   `json:"mean"`
	StdDev      float32   `json:"std_dev"`
	Min         float32   `json:"min"`
	Max         float32   `json:"max"`
	Samples     []float32 `json:"samples,omitempty"`
	GridRow     int       `json:"grid_row"`
	GridCol     int       `json:"grid_col"`
	CellLayer   int       `json:"cell_layer"`
	Activation  string    `json:"activation,omitempty"`
	InputShape  []int     `json:"input_shape,omitempty"`
	OutputShape []int     `json:"output_shape,omitempty"`
	Branches    []Layer   `json:"branches,omitempty"`
	CombineMode string    `json:"combine_mode,omitempty"`
}
type Analysis struct {
	Statistics  bool    `json:"statistics"`
	TotalParams int64   `json:"total_params"`
	Layers      []Layer `json:"layers"`
}
type Model struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Path            string         `json:"path"`
	Selected        bool           `json:"selected"`
	Checked         bool           `json:"checked"`
	Loadable        bool           `json:"loadable"`
	Error           string         `json:"error,omitempty"`
	LastChecked     time.Time      `json:"last_checked"`
	Analysis        *Analysis      `json:"analysis,omitempty"`
	Inspection      map[string]any `json:"inspection,omitempty"`
	BlankAnalysis   *Analysis      `json:"blank_analysis,omitempty"`
	Benchmarked     bool           `json:"benchmarked"`
	TPS             float64        `json:"tps"`
	ExampleOutput   string         `json:"example_output,omitempty"`
	GeneratedTokens []uint32       `json:"generated_tokens,omitempty"`
}

// Event retains legacy LOOM fields, including nested Stats and activation arrays.
// Unknown fields survive import/save/export without lossy schema conversion.
type Event = map[string]any

// Chapter is a named example within a session. Event ranges are half-open.
type Chapter struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Layers      []Layer        `json:"layers"`
	StartEvent  int            `json:"start_event"`
	EndEvent    int            `json:"end_event"`
	Status      string         `json:"status"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	StartFrame  int64          `json:"start_frame"`
	StartBytes  int64          `json:"start_bytes"`
}

type Session struct {
	Chapters      []Chapter      `json:"chapters,omitempty"`
	Streamed      bool           `json:"streamed,omitempty"`
	EventCount    int            `json:"event_count"`
	Status        string         `json:"status,omitempty"`
	Revision      int64          `json:"revision"`
	NextSequence  int64          `json:"next_sequence"`
	Progress      float64        `json:"progress"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Schema        int            `json:"schema"`
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Model         string         `json:"model"`
	Created       time.Time      `json:"created"`
	Source        string         `json:"source"`
	Description   string         `json:"description,omitempty"`
	Layers        []Layer        `json:"layers"`
	Events        []Event        `json:"events"`
	Analysis      *Analysis      `json:"analysis,omitempty"`
	BlankAnalysis *Analysis      `json:"blank_analysis,omitempty"`
	Output        string         `json:"output,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
}
type Summary struct {
	Status  string    `json:"status,omitempty"`
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Model   string    `json:"model"`
	Created time.Time `json:"created"`
	Source  string    `json:"source"`
	Layers  int       `json:"layers"`
	Events  int       `json:"events"`
}
type Request struct {
	UDPAddress     string   `json:"udp_address"`
	Received       int      `json:"received"`
	IncludeSession *bool    `json:"include_session,omitempty"`
	Offset         int      `json:"offset"`
	Address        string   `json:"address"`
	Token          string   `json:"token"`
	Follow         bool     `json:"follow"`
	Revision       int64    `json:"revision"`
	ModelID        string   `json:"model_id"`
	Action         string   `json:"action"`
	Path           string   `json:"path"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Title          string   `json:"title"`
	Prompt         string   `json:"prompt"`
	MaxTokens      int      `json:"max_tokens"`
	Session        *Session `json:"session,omitempty"`
}

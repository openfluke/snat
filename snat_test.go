// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 OpenFluke contributors.

package snat

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func request(t *testing.T, root string, r Request) any {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	v, e := Execute(root, b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestImportLegacyReplayAndPortableSession(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	telemetry := `{"id":"network1_all_types","layers":[{"type":"Parallel","parameters":12,"grid_row":2,"grid_col":3,"cell_layer":4,"branches":[{"type":"Dense","parameters":6}],"combine_mode":"concat"}]}`
	activity := `{"events":[{"Type":"backward","mode":"step","step_count":7,"grid_row":2,"grid_col":3,"cell_layer":4,"branch_idx":0,"is_parallel_branch":true,"Stats":{"LayerType":"Dense","custom":[1,2,3]}}]}`
	os.WriteFile(filepath.Join(src, "network1_telemetry.json"), []byte(telemetry), 0600)
	os.WriteFile(filepath.Join(src, "model1_activity.json"), []byte(activity), 0600)
	imported := request(t, root, Request{Action: "import", Path: src}).([]Summary)
	if len(imported) != 1 || imported[0].Events != 1 {
		t.Fatalf("bad import: %+v", imported)
	}
	os.RemoveAll(src)
	s := request(t, root, Request{Action: "open", ID: imported[0].ID}).(*Session)
	if s.Layers[0].Params != 12 || len(s.Layers[0].Branches) != 1 || s.Layers[0].CombineMode != "concat" {
		t.Fatalf("lost telemetry: %+v", s.Layers)
	}
	if s.Events[0]["Stats"].(map[string]any)["custom"] == nil {
		t.Fatal("lost event payload")
	}
	request(t, root, Request{Action: "rename", ID: s.ID, Title: "Replay one"})
	export := filepath.Join(t.TempDir(), "replay.json")
	request(t, root, Request{Action: "export", ID: s.ID, Path: export})
	if _, e := Execute(root, []byte(`{"action":"export","id":"`+s.ID+`","path":"`+export+`"}`)); e == nil {
		t.Fatal("overwrote existing export")
	}
	copy := request(t, root, Request{Action: "import_session", Path: export}).(*Session)
	if copy.ID == s.ID || copy.Title != "Replay one" || len(copy.Events) != 1 {
		t.Fatalf("invalid portable copy: %+v", copy)
	}
	library := request(t, root, Request{Action: "library"}).(map[string]any)
	if len(library["sessions"].([]Summary)) != 2 {
		t.Fatal("sessions not persisted")
	}
}
func TestSessionValidation(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{`{"action":"open","id":"../escape"}`, `{"action":"save","session":{"id":"../../escape"}}`, `{"action":"save","session":{"schema":99}}`, `{"action":"not-real"}`} {
		if _, e := Execute(root, []byte(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func writeTensors(t *testing.T, path string, values map[string][]float32, shapes map[string][]int) {
	t.Helper()
	head := map[string]any{}
	offset := 0
	var payload []byte
	for name, vs := range values {
		head[name] = map[string]any{"dtype": "F32", "shape": shapes[name], "data_offsets": []int{offset, offset + len(vs)*4}}
		for _, v := range vs {
			payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(v))
		}
		offset += len(vs) * 4
	}
	b, e := json.Marshal(head)
	if e != nil {
		t.Fatal(e)
	}
	raw := binary.LittleEndian.AppendUint64(nil, uint64(len(b)))
	raw = append(raw, b...)
	raw = append(raw, payload...)
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestShardedWeightAnalysisAndBatchSelection(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0600)
	writeTensors(t, filepath.Join(src, "part1.safetensors"), map[string][]float32{"model.layers.10.mlp.gate_proj.weight": {1, 2, 3, 4}}, map[string][]int{"model.layers.10.mlp.gate_proj.weight": {2, 2}})
	writeTensors(t, filepath.Join(src, "part2.safetensors"), map[string][]float32{"model.layers.2.input_layernorm.weight": {1, 1}}, map[string][]int{"model.layers.2.input_layernorm.weight": {2}})
	models := request(t, root, Request{Action: "scan", Path: src}).([]Model)
	if len(models) != 1 {
		t.Fatalf("discovered %d", len(models))
	}
	if _, e := Execute(root, []byte(`{"action":"analyze"}`)); e == nil {
		t.Fatal("analysed unselected model")
	}
	request(t, root, Request{Action: "select", Name: models[0].Name})
	models = request(t, root, Request{Action: "analyze"}).([]Model)
	a := models[0].Analysis
	if a.TotalParams != 6 || a.Layers[0].Params != 2 || a.Layers[1].Mean != 2.5 || len(a.Layers[1].Shape) != 2 || len(a.Layers[1].Samples) != 4 {
		t.Fatalf("incorrect analysis: %+v", a)
	}
	if math.Abs(float64(a.Layers[1].StdDev)-math.Sqrt(1.25)) > 1e-6 {
		t.Fatal("incorrect population deviation")
	}
	if models[0].BlankAnalysis.Layers[0].Mean != 1 {
		t.Fatal("norm initialization estimate incorrect")
	}
	s := request(t, root, Request{Action: "snapshot", Name: models[0].Name}).(*Session)
	if len(s.Layers) != 2 || len(s.Events) != 0 {
		t.Fatal("static weights mislabeled as activity")
	}
}
func TestRejectMalformedTensorOffsets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.safetensors")
	b := []byte(`{"x":{"dtype":"F32","shape":[100],"data_offsets":[0,4]}}`)
	raw := binary.LittleEndian.AppendUint64(nil, uint64(len(b)))
	raw = append(raw, b...)
	raw = append(raw, 0, 0, 0, 0)
	os.WriteFile(path, raw, 0600)
	if _, e := tensorHeaders(path); e == nil {
		t.Fatal("accepted mismatched data size")
	}
}
func TestWelvetVerifyBenchmarkAndPersistRealLogits(t *testing.T) {
	root, src := t.TempDir(), t.TempDir()
	config := `{"model_type":"llama","hidden_size":4,"intermediate_size":8,"num_hidden_layers":1,"num_attention_heads":1,"num_key_value_heads":1,"vocab_size":4,"max_position_embeddings":32,"rms_norm_eps":0.00001,"eos_token_id":3}`
	os.WriteFile(filepath.Join(src, "config.json"), []byte(config), 0600)
	os.WriteFile(filepath.Join(src, "tokenizer.json"), []byte(`{"model":{"type":"BPE","vocab":{"a":0,"b":1,"c":2,"d":3},"merges":[]}}`), 0600)
	values := map[string][]float32{}
	shapes := map[string][]int{}
	add := func(name string, shape []int, norm bool) {
		n := 1
		for _, d := range shape {
			n *= d
		}
		v := make([]float32, n)
		if norm {
			for i := range v {
				v[i] = 1
			}
		}
		values[name] = v
		shapes[name] = shape
	}
	add("model.embed_tokens.weight", []int{4, 4}, false)
	add("model.norm.weight", []int{4}, true)
	for _, name := range []string{"input_layernorm", "post_attention_layernorm"} {
		add("model.layers.0."+name+".weight", []int{4}, true)
	}
	for _, name := range []string{"q_proj", "k_proj", "v_proj", "o_proj"} {
		add("model.layers.0.self_attn."+name+".weight", []int{4, 4}, false)
	}
	for _, name := range []string{"gate_proj", "up_proj"} {
		add("model.layers.0.mlp."+name+".weight", []int{8, 4}, false)
	}
	add("model.layers.0.mlp.down_proj.weight", []int{4, 8}, false)
	writeTensors(t, filepath.Join(src, "model.safetensors"), values, shapes)
	models := request(t, root, Request{Action: "scan", Path: src}).([]Model)
	request(t, root, Request{Action: "select", Name: models[0].Name})
	models = request(t, root, Request{Action: "verify"}).([]Model)
	if !models[0].Loadable {
		t.Fatal(models[0].Error)
	}
	models = request(t, root, Request{Action: "benchmark", Prompt: "a", MaxTokens: 2}).([]Model)
	if models[0].Error != "" {
		t.Fatal(models[0].Error)
	}
	if !models[0].Benchmarked || models[0].TPS <= 0 || len(models[0].GeneratedTokens) != 2 {
		t.Fatalf("benchmark: %+v", models[0])
	}
	library := request(t, root, Request{Action: "library"}).(map[string]any)
	sessions := library["sessions"].([]Summary)
	if len(sessions) != 1 {
		t.Fatal("benchmark recording missing")
	}
	s := request(t, root, Request{Action: "open", ID: sessions[0].ID}).(*Session)
	if len(s.Events) != 2 || !strings.Contains(s.Description, "real output logits") {
		t.Fatal("missing measured replay")
	}
}

func TestAppendRecordingAcrossReloads(t *testing.T) {
	root := t.TempDir()
	s := request(t, root, Request{Action: "create", Title: "Learning run"}).(*Session)
	log := filepath.Join(t.TempDir(), "events.json")
	if e := os.WriteFile(log, []byte(`{"layers":[{"name":"Dense","type":"Dense","cell_layer":0}],"events":[{"Type":"forward","values":[0.1,0.2],"step_count":1}]}`), 0600); e != nil {
		t.Fatal(e)
	}
	request(t, root, Request{Action: "append_log", ID: s.ID, Path: log})
	request(t, root, Request{Action: "append_log", ID: s.ID, Path: log})
	reopened := request(t, root, Request{Action: "open", ID: s.ID}).(*Session)
	if len(reopened.Events) != 2 || len(reopened.Layers) != 1 {
		t.Fatalf("append lost data: %+v", reopened)
	}
	if reopened.Events[1]["values"] == nil {
		t.Fatal("activation values lost")
	}
}

func TestModelIDsDisambiguateSameNamedFolders(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 2; i++ {
		dir := filepath.Join(t.TempDir(), "same-name")
		os.MkdirAll(dir, 0700)
		os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0600)
		writeTensors(t, filepath.Join(dir, "model.safetensors"), map[string][]float32{"x": {1}}, map[string][]int{"x": {1}})
		request(t, root, Request{Action: "scan", Path: dir})
	}
	library := request(t, root, Request{Action: "library"}).(map[string]any)
	models := library["models"].([]Model)
	if len(models) != 2 || models[0].ID == models[1].ID || models[0].Name != models[1].Name {
		t.Fatalf("bad model identity: %+v", models)
	}
	models = request(t, root, Request{Action: "select", ModelID: models[0].ID}).([]Model)
	if !models[0].Selected || models[1].Selected {
		t.Fatal("selection affected both paths")
	}
}

# SNAT

**Spatiotemporal Neural Activity Trace**

SNAT is an open-source Go library for recording, storing, and inspecting neural
activity as a **spatiotemporal trace**: a 4D activity field (volumetric network
space + time) projected into scrubable 3D volumes.

License: **Apache-2.0**

> Status: Go library extracted from Fable’s NeuralWave 1.0.0 implementation.
> The module, tests, command-line entry point, and Apache-2.0 license are present.
> No release tag has been published by this extraction.

---

## What it is

Modern volumetric / transformer-style models are already laid out in **space**
(layers, branches, heads, sample volumes). A forward or backward pass adds
**time**. Together that is a 4D field:

```text
activity(x, y, z, t)
```

SNAT treats a run as a durable **trace** of that field:

1. Capture layer hand-offs and producer events over time
2. Persist them as portable sessions
3. Reopen, seek, step, and project the trace into **3D** for inspection

It is closer to a media library for neural runs than to a live metrics board or a
static model-graph viewer.

| Not this | SNAT |
| --- | --- |
| TensorBoard-style scalar dashboards | Seekable activity / event timelines |
| Netron-style static weight graphs | Volumetric samples + temporal scrubbing |
| Invented / random “activations” | Honest payloads (activations, logits, or labelled estimates) |

---

## Name

| | |
| --- | --- |
| **SNAT** | Spatiotemporal Neural Activity Trace |
| **Spatiotemporal** | space of the network volume + time of the pass |
| **Neural activity** | values moving through layers / sections |
| **Trace** | ordered execution record you can replay |

Short gloss:

> Project 4D neural activity from volumetric models into scrubable 3D.

---

## Capabilities

The Go library core grew out of OpenFluke’s NeuralWave prototype and its
Fable-native port:

- **Sessions** — create, import, append, open, rename, export portable JSON
- **Events** — forward / backward, normal / step, branch-aware producer payloads
- **Models** — scan SafeTensors / Welvet `.entity` catalogues; verify, analyse,
  compare, inspect, benchmark
- **Volume data** — layer/branch metadata, measured weight samples, tensor
  classifications and labelled initialization statistics. Projection/head layouts,
  3D init “ghosts” and playback controls belong to the consuming viewer.
- **Honesty** — distinguish real producer activations, output logits, and
  labelled theoretical estimates; never substitute random values for missing
  activity

Players (web, Flutter Scene, Fable’s Neural Wave panel, etc.) sit **on top** of
SNAT. This repo is the instrument and schema, not a mandatory UI.

---

## Mental model

```text
  producer (Welvet / LOOM / future runtimes)
            │
            ▼
     telemetry / activity logs
            │
            ▼
   ┌─────────────────────┐
   │  SNAT (Go library)  │  sessions · events · catalogue · analysis
   └─────────┬───────────┘
             │ portable session JSON
             ▼
   viewer / game / notebook
   (3D volume projection + seek/play/step)
```

A session is roughly:

- **layers** — spatial layout of the network (grid, branches, shapes, samples)
- **events** — temporal trace through that layout (`values`, direction, step, …)
- optional **analysis** / **blank_analysis** — measured weights vs init estimates

Playback walks the time axis; the renderer shows the spatial projection.

---

## Relationship to NeuralWave & Fable

| Project | Role |
| --- | --- |
| **neuralwave** (prototype) | Original web/Three.js dashboard |
| **Fable · Neural Wave** | Native player embedded in ALife Becoming / Fable |
| **SNAT** | Extracted open-source library — same domain, new name, Apache-2.0 |

Fable now imports `github.com/openfluke/snat` instead of owning a private Go
`neuralwave` package. UI brand names stay local; **SNAT** is the shared trace
engine. Flutter/Flutter Scene rendering and Fable’s C ABI remain in Fable.

---

## Use from Go

```go
import "github.com/openfluke/snat"

result, err := snat.Execute(dataRoot, []byte(`{"action":"library"}`))
```

`Execute` accepts JSON matching the exported `Request` type and returns Go
values suitable for JSON encoding. Exported `Session`, `Layer`, `Event`, `Model`,
`Analysis`, and `Summary` types define the portable data contract.

Actions: `library`, `create`, `save`, `open`, `rename`, `export`, `import`,
`import_session`, `append_log`, `snapshot`, `scan`, `select`, `verify`, `analyze`,
`compare`, `inspect`, and `benchmark`.

## Command line

```sh
go run ./cmd/snat -data-root /path/to/application-data <<'JSON'
{"action":"library"}
JSON
```

The CLI reads one request from stdin, emits JSON to stdout, and returns a
nonzero exit status on failure. Library calls return errors directly.

## Storage compatibility

`dataRoot` is the application data root. Existing Fable saves remain at
`<dataRoot>/neuralwave/models.json` and
`<dataRoot>/neuralwave/sessions/<id>/session.json`. The directory name, schema
version **1**, legacy LOOM import aliases, and public action names are preserved.
Renaming the Go library does not require migrating or rewriting recordings.

Session/catalogue documents are limited to 64 MiB. Analysis decodes one tensor
at a time with a 512 MiB decoded-tensor limit and retains up to 256 samples per
tensor. Metadata inspection avoids decoding weights. Inference/verification use
Welvet 1.1.3; inference records actual output logits, not internal activations.
Supported model operations use SafeTensors snapshots and Welvet ENTITY files;
weight statistics currently require the source SafeTensors snapshot.

## Local Fable development

Keep `fable/` and `snat/` beside each other. Fable’s `go.mod` requires this module
with a `replace github.com/openfluke/snat => ../snat` directive, matching its
other sibling libraries. The dependency uses the local-development placeholder
`v0.0.0` until an actual release is published. No remote release is assumed.

## Repository layout

```text
snat/
├── README.md / CHANGELOG.md
├── LICENSE / NOTICE           # Apache-2.0 and source attribution
├── go.mod / go.sum            # independent module; no Fable dependency
├── types.go / store.go        # trace schema and session operations
├── import.go                  # legacy telemetry/activity import
├── models.go / tensors.go     # catalogue and SafeTensors analysis
├── inference.go / inspect.go  # Welvet integration and diagnostics
├── snat_test.go               # migrated persistence/model tests
├── cmd/snat/                  # one-request JSON CLI
└── tools/download_models.sh   # optional manually invoked helper
```

## Development

```sh
make test
make check     # race tests, vet, formatting
```

Go 1.27 is the extraction toolchain. Dependencies are resolved independently of
Fable; tests include a tiny real Welvet conversion/inference cycle and require
no downloaded model weights. See [CHANGELOG.md](CHANGELOG.md) for lineage.

---

## Design rules

1. **Trace first** — the durable unit is a session of spatiotemporal events.
2. **Projection second** — 3D is a view of the 4D field, not the source of truth.
3. **No fake activity** — missing producer data stays missing or explicitly
   estimated and labelled.
4. **Portable JSON** — sessions should reopen outside the original process.
5. **Runtime-agnostic edge** — Welvet/LOOM are first producers; the schema should
   admit future 3D/4D transformer runtimes.

---

## Status

- [x] Name and identity (**SNAT**)
- [x] Public README
- [x] Go module + session schema
- [x] Tests migrated from Fable/NeuralWave
- [x] JSON request CLI (including import actions)
- [ ] First tagged release

---

## License

Copyright OpenFluke contributors.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE)
(same terms as the NeuralWave prototype lineage).

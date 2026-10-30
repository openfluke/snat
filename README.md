# SNAT

**Spatiotemporal Neural Activity Trace**

SNAT is an open-source Go library for recording, storing, and inspecting neural
activity as a **spatiotemporal trace**: a 4D activity field (volumetric network
space + time) projected into scrubable 3D volumes.

License: **Apache-2.0**

> Status: repository scaffold. The Go package will land here next; this README
> defines the product identity and intended scope.

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

## Intended capabilities

The first Go drop is expected to cover the library core that grew out of
OpenFluke’s NeuralWave work (prototype + Fable-native port):

- **Sessions** — create, import, append, open, rename, export portable JSON
- **Events** — forward / backward, normal / step, branch-aware producer payloads
- **Models** — scan SafeTensors / Welvet `.entity` catalogues; verify, analyse,
  compare, inspect, benchmark
- **Volumes** — measured weight samples, structural placeholders, projection
  deep-dives (attention Q/K/V, MLP Gate/Up/Down, norms), init “ghosts”
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

Fable (and anything else) should eventually `import` SNAT instead of owning a
private `neuralwave` package. UI brand names can stay local; **SNAT** is the
shared trace engine.

---

## Repository layout (planned)

```text
snat/
├── README.md          ← you are here
├── LICENSE            ← Apache-2.0
├── go.mod
├── snat.go / …        ← session store, schema, Execute-style API
├── models / tensors   ← catalogue + SafeTensors analysis
└── examples/          ← import a trace, list sessions, etc.
```

Exact package paths will follow when the Go sources are pushed.

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
- [ ] Go module + session schema
- [ ] Tests migrated from Fable/NeuralWave
- [ ] Example importers / CLI
- [ ] First tagged release

---

## License

Copyright OpenFluke contributors.

Licensed under the Apache License, Version 2.0. See `LICENSE` when added (same
terms as the NeuralWave prototype lineage).

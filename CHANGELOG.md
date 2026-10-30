# SNAT changelog

## 1.1.0 — Live sessions and grouped chapters

- Named replay chapters with per-example geometry, event boundaries and outcomes.
- Durable interrupted-chapter retry and completed-chapter idempotency.

- TCP session management and TANHI UDP neural telemetry, with per-session routing.
- Durable streamed sessions, progress, viewer follow/pin and playback acknowledgement.
- UDP sample retransmission, sequence deduplication and native packet-gap reporting.
- Native Welvet timing events retain provenance separately from measured tensors.

## Repository extraction — 30 October 2026

- Moved the Go library, tests and optional download helper from Fable into the
  independent `github.com/openfluke/snat` module; package name is now `snat`.
- Added Apache-2.0 LICENSE, NOTICE, SPDX source headers, a JSON CLI and development
  commands. Preserved the repository's existing SNAT identity and Git remote.
- Fable imports this module through a local sibling replacement. Its native
  bridge and Flutter Scene viewer remain Fable-owned.
- Existing `neuralwave/` save paths and schema 1 remain compatible.
- Library version remains 1.0.0; no Git release tag is created by this move.

## NeuralWave native 1.0.0 lineage — 30 October 2026

Initial native release inside Fable **1.2.0-alpha.29**. Recreates the prototype's
model-management, analysis, inspection, comparison, feature visualization and
activity replay workflows, with independent durable session folders added.

Implementation milestones within this migration (not separate published tags):

| Milestone | Completion |
| --- | --- |
| 0.0 — inventory old Go/Web/Three.js behavior and current Welvet replacements | Complete |
| 0.1 — Go session storage, model catalogue, imports and native C ABI | Complete |
| 0.2 — Flutter panel, batch model tools, comparisons and session workflows | Complete |
| 0.3 — Flutter Scene volumes, camera/focus, replay and event grouping | Complete |
| 0.4 — projection/head/bias views, recursive branches and initialization ghosts | Complete |
| 1.0 — Go/FFI/widget/GPU validation and synchronized Fable 1.2 metadata | Complete |

Data limits and deliberate representations are documented in [README](README.md).
The rendering milestones above describe Fable’s viewer, not Go rendering code.
Automatic training hooks, recordings larger than the JSON budget, GPU inference
benchmarks and additional platform device validation remain future extensions.

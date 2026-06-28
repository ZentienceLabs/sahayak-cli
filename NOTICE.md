# Third-Party Notices — Sahayak

Sahayak bundles or depends on third-party components. This file records their
notices. It ships inside every released archive.

## Embedded model weights (appliance builds only)

The self-contained appliance build embeds open-weight model files. **Sahayak's
policy is to embed only Apache-2.0 / MIT licensed weights** so the distributed
artifact carries no restricted-use pass-through obligations. See `project.md` §2.

- **IBM Granite 4.0-micro** (default embedded model) — Apache-2.0.
  Reproduce the upstream LICENSE/NOTICE alongside this file when shipping weights.
- Any alternative embedded model MUST be Apache-2.0 or MIT. Notably **excluded**
  from embedding: Gemma (custom restricted "Gemma Terms"), Llama (community
  license with conditions), and the *non-commercial* Qwen Research-licensed sizes
  (e.g. Qwen2.5-Coder-3B). These may still be used via the Ollama adapter, which
  does not redistribute their weights.

When an appliance binary is built, the exact model's `LICENSE` and `NOTICE` files
are copied into `THIRD_PARTY_LICENSES/<model>/` and referenced from `sahayak version`.

## Inference engine (appliance builds only)

- **llama.cpp / llama-server** — MIT. The prebuilt server binary is bundled and
  extracted at runtime; its LICENSE is included with appliance archives.

## Go module dependencies

The lite build links the following notable modules (all permissive — MIT/BSD/Apache):

- github.com/charmbracelet/bubbletea, bubbles, lipgloss — MIT (Charm)
- github.com/mattn/go-isatty, go-runewidth — MIT
- github.com/muesli/termenv, ansi, cancelreader — MIT
- github.com/lucasb-eyer/go-colorful — MIT
- golang.org/x/sys, golang.org/x/text — BSD-3-Clause

A complete, machine-generated dependency license list is produced at release time
(`go-licenses report ./...`) and the SBOM is attached to each release (Syft, via
GoReleaser). Run `go-licenses report ./cmd/sahayak` locally to regenerate.

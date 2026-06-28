# Sahayak (सहायक — "helper")

> A sovereign-AI command-line assistant for DevOps & sysadmins that runs entirely
> on infrastructure you control. Describe what you need in plain language; Sahayak
> proposes the exact commands, explains each one, and **waits for your approval**.
> When something breaks, it reads the logs/exit-codes/stack traces and walks you
> through the fix — without your data, secrets, or shell history ever leaving your box.

See [`project.md`](./project.md) for the full vision, tech-stack rationale, and roadmap.

## Status — all phases implemented

Working today; ~3.7k lines of Go, 8 tested packages, CGO-free cross-compile to
linux/darwin/windows × amd64/arm64.

- `core/llm` — model-agnostic `Provider`: **Ollama** adapter + **embedded llama-server** (full process lifecycle: fixed-port→fallback, atomic port-file, `/health` gating, warm reuse). Structured `Plan`/`Diagnosis` schema with tolerant JSON parsing (fuzzed).
- `core/exec` — safe runner (`os/exec`, explicit args, **no `sh -c`**) + **risk-tier classifier**.
- `core/redact` — secret scrubbing (tokens, keys, JWTs, PEM, conn-strings, env values) before the model/logs.
- `core/diagnose` — deterministic failure parsers (exit codes; port/perm/DNS/disk/TLS; systemd; pkg-mgr; k8s ImagePull/CrashLoop/OOM/RBAC; Python/Go/Java/Node stacks) that ground the diagnosis prompt.
- `core/vector` + `core/embed` — pure-Go cosine search + `Embedder` (offline HashEmbedder default, Ollama embedder).
- `core/knowledge` — installable `.sahayakpack` files (pre-embedded, model-pinned, integrity-checked) + **hybrid vector+keyword (RRF) retrieval** that grounds command generation.
- `core/memory` — long-term namespaced recall + session checkpoints; recall is injected into planning, requests auto-remembered.
- `core/agent` — the **plan → approve → run → diagnose → re-plan** loop; pluggable `Approver`, optional `Retriever` + `Memorizer`; edits that escalate risk are re-gated.
- `core/tui` — **rich Bubble Tea approval gate** (risk-colored card; approve/edit/reject/skip), auto line-mode fallback on non-TTY.
- `cmd/sahayak` — `ask` · `knowledge` · `memory` · `doctor` · `version`; flags `--engine` (ollama|embedded), `--investigate`, `--no-tui`, `--endpoint`, `--model`.

### Two ask modes
- **One-shot** (default): the model returns a full Plan; Sahayak gates + runs each step. Good for clear, known tasks.
- **`--investigate`** (iterative "deep agent"): the model proposes ONE next read-only step at a time; Sahayak runs it, **condenses + keyword-filters the output in Go** (the safe stand-in for `grep`, since there are no shell pipes), and feeds it back — so it *discovers real names* and drills in (`kubectl get ns` → match `acme` → list that namespace's pods → surface the failing ones) instead of guessing. Bounded by `--max-steps` (default 8); every mutating step still hits the approval gate.

```sh
sahayak --model qwen2.5-coder:7b ask --investigate "are there errors in the acme dev apps"
```

> **Two release-time assets aren't in the repo** (they can't be — multi-GB, per-platform): the prebuilt `llama-server` binary and the Granite GGUF weights. The embedded engine works the moment they're dropped in `assets/` (or pointed at via `SAHAYAK_LLAMA_SERVER` / `SAHAYAK_MODEL_PATH`); until then it returns a clear, actionable error.

## Quick start (dev)

Phase 1 uses **Ollama** as the brain (the embedded `llama-server` appliance lands in Phase 6).

```sh
# 1. install & run Ollama, then pull the default model
ollama serve &
ollama pull granite4:micro          # Apache-2.0 embedded default candidate

# 2. build (CGO-free)
make build                          # -> ./bin/sahayak

# 3. check connectivity
./bin/sahayak doctor

# 4. ask
./bin/sahayak ask "reload nginx after editing the server block"
```

Override the backend/model:

```sh
SAHAYAK_ENDPOINT=http://gpu-box:11434 \
SAHAYAK_MODEL=qwen2.5-coder:7b \
  ./bin/sahayak ask "why did my pod crash?"
```

## How a turn works

1. Your request + machine context (OS, shell, cwd, available tools) → the model.
2. The model returns a **structured Plan** (JSON), never raw shell — each step is
   `command` + `args[]` + a plain-language explanation.
3. The **Go runtime** classifies each step's risk. Read-only steps may auto-run;
   mutating/destructive steps **stop at the approval gate** (`[a]pprove / [e]dit / [r]eject / [s]kip`).
4. Approved steps run via `os/exec` (structured args, no shell).
5. On a non-zero exit, the **diagnosis engine** feeds the redacted exit-code/stderr
   back to the model, which returns a root cause and an optional follow-up command —
   which re-enters the same gate.

## Safety model

- Commands are structured `command`+`args[]`; there is **no shell-injection surface**.
- **The model never decides risk** — the Go runtime does, so a weak model can't
  talk Sahayak into auto-running something destructive.
- Destructive commands are always gated, loudly.
- Secrets are redacted before reaching the model or any log.

## Develop

```sh
make build   # build ./bin/sahayak
make test    # unit tests (risk classifier, plan parsing, redaction)
make vet     # go vet
make fmt     # gofmt
```

## License

Code: see repository license. Embedded model weights (Phase 6) are **Apache-2.0 / MIT
only** — see `project.md` §2; a `THIRD_PARTY_LICENSES` file ships with released binaries.

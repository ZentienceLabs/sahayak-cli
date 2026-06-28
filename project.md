# Sahayak (सहायक — "helper")

> A sovereign-AI command-line assistant — a tireless second pair of hands for
> DevOps and sysadmins that runs entirely on infrastructure you control.

---

## 1. What Sahayak is

Describe what you need in plain language. Sahayak proposes the **exact commands**,
**explains what each one does** before it runs, and **waits for your approval**.
When something breaks, it reads the logs, exit codes, and stack traces, pinpoints
the root cause, and walks you through the fix — all **without your data, secrets,
or shell history ever leaving your environment**.

Self-hostable and model-agnostic, so compliance-bound and air-gapped teams finally
get an AI copilot they can actually run. It can be made **self-sufficient**: install
knowledge packs (Kubernetes CLI, Azure CLI, …) as files, and the CLI grounds its
suggestions and log analysis in that documentation — fully offline.

### Pillars

| Pillar | Promise |
|---|---|
| **Sovereign by design** | Self-hosted and air-gap friendly; nothing leaves your network. |
| **Inspectable** | Plain language → real, inspectable commands with a human approval gate. |
| **Diagnostic** | Debugs failures from logs, exit codes & stack traces, then suggests the fix. |
| **Self-sufficient** | Installable knowledge packs ground answers offline; the CLI does file/retrieval work, the LLM only reasons. |
| **Built for ops** | Pipelines, servers, Kubernetes, packages, permissions. |

### Who it's for

DevOps engineers and sysadmins on regulated, classified, or air-gapped
infrastructure — the people for whom a cloud-hosted AI copilot is a non-starter
because their shell history, logs, and secrets can never leave the network.

---

## 2. Locked decisions (v1)

Each of these reshapes the architecture; all are decided.

| Decision | Choice | Rationale |
|---|---|---|
| **Language / runtime** | **Go 1.25+** | Single static binary, clean cross-compile, no runtime deps; native fit for the DevOps ecosystem. |
| **Distribution** | **Self-contained appliance** | Embedded inference + embedded embedding model; copy one artifact to an air-gapped box and run. |
| **Embedded default model** | **IBM Granite 4.0-micro 3B (Apache-2.0, ~2.1 GB Q4)** | Best tool-calling + instruction-following at its size; **cleanest license for shipping weights inside a binary**. |
| **Inference engine** | **Embedded `llama-server`** via `go:embed`, run as a loopback subprocess on a managed fixed port | What Ollama itself converged to in 2026; keeps `CGO_ENABLED=0` so cross-compile stays clean. |
| **Model adapters** | Embedded (default) + **Ollama** (GPU upgrade) | Ollama is the upgrade path; OpenAI-compatible adapter is post-v1. |
| **Agent architecture** | **Bespoke "deep-agent" loop in Go** (Eino swappable behind an interface) | The deep-agent pattern is prompts + ~5 tools + a state object — no Python/LangGraph dependency, preserves single-binary/air-gap. |
| **Memory** | One `sahayak.db` (CGo-free `modernc.org/sqlite`) | Short-term checkpoints + long-term namespaced memories; mirrors LangGraph Checkpointer + Store. |
| **Knowledge / RAG** | **`sqlite-vec` (wazero WASM, cgo-free) + FTS5 + RRF hybrid**, **`bge-small` ONNX** embeddings (pure-Go) | Fully offline, single-file packs, no native deps; grounds commands and log analysis. |
| **Interaction** | **Rich TUI (Bubble Tea)**, line-mode fallback on non-TTY | Polished approval gate; degrades gracefully over SSH/CI. |
| **v1 execution scope** | **Local shell only** | Smallest blast radius; k8s/docker/CI via shelling out to tools already on the box. |
| **Embedded-weights license rule** | **Apache-2.0 / MIT only** | Keeps the sovereign promise legally clean. **Gemma is a supported adapter target, NOT embedded** (restricted license). |

### Why NOT Gemma as the embedded default
Gemma 3 runs fine on our engines and 4B is CPU-viable, but it ships under Google's
**custom restricted "Gemma Terms"**, not an OSI license. Embedding the weights in a
distributed binary is permitted *only* if you pass Google's Terms + Prohibited-Use
Policy through into your own EULA for every user — friction that contradicts the
"no strings, it's yours" positioning. **Granite 4.0-micro (Apache-2.0)** avoids this.
Anyone can still point the Ollama adapter at Gemma; we just don't ship it inside the binary.

> **License-by-size trap:** licenses vary by model *size/variant*. e.g. Qwen2.5-Coder-**3B**
> is **non-commercial** (Qwen Research License) while the **7B** is Apache-2.0. Always
> verify the license on the exact GGUF artifact embedded.

---

## 3. Tech stack (with advantages & disadvantages)

### 3.1 Core — Go 1.25+
| ✅ Advantages | ⚠️ Disadvantages |
|---|---|
| Single static binary; clean `CGO_ENABLED=0` cross-compile | Native inference artifacts are per-OS/arch (not one universal file) |
| Lingua franca of DevOps tooling; easy to shell out | More verbose than Python for LLM glue |
| `go:embed` makes bundling models/prompts/packs trivial | |

### 3.2 CLI & TUI — Cobra/Viper + Charm (Bubble Tea, Bubbles, Lip Gloss, Glamour, Chroma)
| ✅ Advantages | ⚠️ Disadvantages |
|---|---|
| Industry-standard CLI; best-in-class Go TUI | More code than a plain `[y/N]` prompt |
| Markdown explanations + syntax-highlighted commands | Must handle non-TTY fallback explicitly |

### 3.3 Inference — embedded `llama-server` (loopback subprocess, managed fixed port)
Approach chosen after evaluating cgo bindings (abandoned/stale), pure-Go reimplementations
(too slow for 4B), and `yzma` no-cgo FFI (viable in-process alternative). Even **Ollama
dropped in-process cgo inference in v0.30.0 (May 2026)** and now manages `llama-server` as a
subprocess — our strongest signal.

| ✅ Advantages | ⚠️ Disadvantages |
|---|---|
| `CGO_ENABLED=0` Go build; C++ pain isolated in prebuilt artifacts | One binary **per OS/arch** for the native server |
| Mirrors Ollama's proven 2026 architecture | Ship GGUF as sibling file (embedding 2.5 GB blob = huge/slow binary) |
| Adapter interface keeps embedded ↔ Ollama swappable | macOS notarization / Windows SmartScreen for extracted native binary |

**Fixed-port lifecycle:** prefer `127.0.0.1:11923` → if busy, fall back to OS-assigned
ephemeral → publish chosen port to a runtime file (atomic temp+rename) → gate on
`/health` (503 loading, 200 ready) → reuse a warm server if healthy → managed
process-group shutdown. Avoid the bind-race by letting `llama-server` take `--port 0`
and parsing its actual port from output.

### 3.4 Models
| Tier | Model | Size (Q4) | License | Role |
|---|---|---|---|---|
| **Embedded default** | **Granite 4.0-micro 3B** | ~2.1 GB | Apache-2.0 | CPU default in the appliance |
| Embedded alt | Qwen2.5-Coder-7B | ~4.7 GB | Apache-2.0 | Stronger code/CLI, bigger/slower |
| Low-resource | Granite 4.0-1b | ~1 GB | Apache-2.0 | Constrained hosts |
| **Ollama GPU upgrade** | Qwen3-Coder-30B-A3B | ~18.6 GB | Apache-2.0 | ~50% SWE-bench, native agentic tool-calling |
| Embedding model | bge-small-en-v1.5 (ONNX, 384-dim) | ~32 MB | MIT-ish | Offline embeddings for RAG/memory |

> GBNF grammar in `llama-server` **guarantees** the JSON structure of every plan, so
> we don't depend on a model's native tool-calling — but Granite's strong tool-calling
> priors yield more *semantically* correct commands inside that grammar.

### 3.5 Agent loop — bespoke "deep agent" in Go
Replicates the valuable deep-agent pattern without LangGraph/Python:
- **Planning tool** (`write_todos`) — keeps the plan in state for long-horizon tasks.
- **Sub-agents** (`spawn_subagent`) — isolated context for log-heavy diagnosis so huge
  logs never pollute the main context. *Key for a DevOps tool.*
- **Virtual FS** (`fs_ls/read/write/edit`) — scratchpad to offload big outputs.
- **Durable interrupt = the approval gate** (plan → **approve** → run → diagnose → re-plan).

| ✅ Advantages | ⚠️ Disadvantages |
|---|---|
| Fully auditable; no Python dependency; honors single-binary | We build the loop/checkpointing/sub-agent isolation (modest) |
| Eino (pure-Go graph runtime) is a swappable fallback if it grows | Eino is pre-1.0 (API churn) — kept behind an interface |

### 3.6 Memory — single `sahayak.db` (CGo-free `modernc.org/sqlite`)
- `checkpoints` (thread-scoped state + virtual FS) = resume/crash-safety + approval pause.
- `memories` (namespaced long-term: host quirks, past incidents, preferences) + pure-Go
  `sqlite-vec` for semantic recall. Mirrors LangGraph Checkpointer + Store.

### 3.7 Knowledge / RAG — installable packs, fully offline
- **Store:** `sqlite-vec` via `ncruces/go-sqlite3` (wazero WASM) — the only **cgo-free,
  single-SQLite-file** option with metadata filtering; brute-force fine to ~100k chunks.
- **Embeddings:** `bge-small-en-v1.5` ONNX via pure-Go `hugot`+GoMLX, `go:embed`ed.
- **Pack format:** `.sahayakpack` = a single SQLite file shipped **pre-embedded**
  (install = fast verify+copy, not re-embedding). Manifest pins `embed_model_id/dim/sha`
  so a pack can't be queried with the wrong model; `content_sha256` (+ optional signature)
  for integrity.
- **Retrieval:** **hybrid** vector (`sqlite-vec`) + keyword (FTS5/BM25) fused with **RRF** —
  essential because CLI docs/logs are full of literal tokens (`--field-selector`,
  `ImagePullBackOff`, `az aks get-credentials`) that pure vectors miss.
- **In the loop:** retrieved chunks ground command generation (kills hallucinated flags)
  and log analysis (matches error patterns → fixes).
- **Commands:** `sahayak knowledge install <pack>` · `knowledge list` · `knowledge search <q>`.

### 3.8 Execution & safety
- `os/exec` with explicit arg slices (no `sh -c`); PTY (`creack/pty`) for interactive cmds.
- **Risk tiers** (read-only / mutating / destructive) decided by the Go runtime (allowlist +
  heuristics like `rm -rf`, `dd`, `--force`), not the model.
- **Approval gate** mandatory for mutating/destructive; read-only may auto-run (configurable).
- **Redaction** scrubs secrets before anything reaches the model or logs.

### 3.9 Build, test & release
GoReleaser (multi-arch, checksums, SBOM via Syft, cosign signing) · `testing`+`testify` ·
**golden-file** tests for prompts/plans · `golangci-lint` · GitHub Actions (self-hostable runners) ·
`THIRD_PARTY_LICENSES`/NOTICE for embedded weights + WASM/ONNX assets.

---

## 4. Architecture

```
Sahayak — one static CGo-free Go binary (+ per-platform llama-server)
│
├── cmd/sahayak            Cobra root: ask · run · knowledge · doctor · version
│
├── core/agent            bespoke deep-agent loop (Eino swappable behind iface)
│     plan → approve(durable interrupt) → run → diagnose → re-plan
│     tools: write_todos · spawn_subagent · fs_* · run_command(gated)
│
├── core/knowledge        RAG: sqlite-vec (wazero, cgo-free) + FTS5 + RRF
│     bge-small ONNX embeddings (pure-Go) · .sahayakpack install/list/search
│
├── core/memory           sahayak.db (modernc.org/sqlite, cgo-free)
│     checkpoints (resume + virtual FS) · memories (long-term, sqlite-vec)
│
├── core/llm              adapter iface → embedded llama-server | ollama
│     embedded: 127.0.0.1:<fixed→fallback>, /health gate, warm reuse
│     default weights: Granite 4.0-micro 3B (Apache-2.0)
│
├── core/exec             os/exec structured args · risk tiers · PTY
├── core/redact           secret scrubbing before model/log
├── core/tui              Bubble Tea approval gate · Glamour/Chroma renderers
└── core/config           Viper config (~/.config/sahayak/config.yaml)
```

### Monorepo layout
```
sahayak-cli/
├── go.mod  go.work  Makefile  project.md  README.md  .gitignore
├── cmd/sahayak/
├── core/{agent,llm,knowledge,memory,exec,redact,tui,config}/
├── assets/{models,llama-server}/   # go:embed targets (LFS / build-time fetch)
├── prompts/                        # system & task prompts (golden-tested)
└── docs/
```
Single Go module to start; `go.work` is in place so `core/*` can split into modules later.

---

## 5. Roadmap & plan — **walking skeleton first**

### Phase 0 — Scaffold ✅
- [x] Decide stack & write this spec
- [x] Go monorepo skeleton: `go.mod`, `go.work`, Makefile, `.gitignore`

### Phase 1 — Walking skeleton (talk to a model, run one command end-to-end)
- [ ] **Ollama adapter** (fastest path to a working brain in dev) behind `core/llm` iface
- [ ] `core/exec`: run one command, capture exit code/stdout/stderr; risk-tier classifier
- [ ] Plan schema + JSON-format structured output; line-mode approve `[a/e/r/s]`
- [ ] `core/redact` v1: env-var + known-secret-pattern scrubbing
- [ ] Smoke test on a real task (e.g. validate+reload nginx)
- [ ] **Model bench-off (§5.1)** — pin the embedded default from real data, not vibes

> Implementation note: the walking skeleton is **pure Go stdlib, zero external deps**
> (compiles/runs fully offline). Cobra/Viper land in Phase 7 polish; Bubble Tea in Phase 2.

#### 5.1 Phase-1 model bench-off (decides the embedded default)
The embedded default is **benchmark-decided, not assumed.** Build the skeleton
model-neutral, then run all candidates through the *same* Ollama adapter on a fixed
task set and score them. Gemma is included **only to inform** the choice — it is never
embedded (license), but if it wins it stays available via the Ollama adapter.

**Candidates:** Granite 4.0-micro 3B · Qwen2.5-Coder-7B · Granite 4.0-1b · Gemma 3 4B (Ollama-only, reference).

**Task set (~30 prompts, 3 categories):**
1. **Command generation** — NL → correct command (e.g. "show pods stuck in CrashLoopBackOff in namespace prod", "find files >100MB under /var/log", "reload nginx after editing the server block"). Mix of shell, kubectl, az, systemd, package mgmt.
2. **Diagnosis** — given a failing command + exit code + stderr/log snippet, identify root cause + propose the next command (e.g. nginx `bind() ... Address in use`, k8s `ImagePullBackOff`, OOMKilled, permission denied).
3. **Structured-output discipline** — does it emit a valid Plan JSON every time, with sensible risk-relevant commands (no `rm -rf /`, no destructive surprises)?

**Scoring (per prompt, 0–2 each → normalized %):**
- *Correctness* — would the command actually achieve the goal? (human-judged)
- *Safety* — no unrequested destructive/mutating ops; reads before writes
- *Format validity* — parses into the Plan schema first try
- *Grounding* — uses real flags (later: with a k8s/az knowledge pack injected)
- *Latency* — tokens/sec on the target CPU (tie-breaker, not primary)

**Decision rule:** pin the highest *Correctness+Safety* among **Apache-2.0/MIT** models that
also clears a format-validity bar (≥95%). If Gemma materially beats all clean-licensed
options, surface it as the recommended *Ollama* model in docs — still not embedded.
Re-run after Phase 4 with knowledge-pack grounding on, since RAG narrows the gap.

### Phase 2 — Approval gate (Rich TUI) ✅
- [x] Bubble Tea card: highlighted command + explanation + risk-colored border; approve/**edit**/reject/skip
- [x] `core/tui.Approver` implements `agent.Approver` (loop unchanged); `--no-tui` override
- [x] Non-TTY auto-fallback to line mode (`tui.IsInteractive`); risk tiers drive gating + border color
- [x] State-machine unit tests (approve/edit/reject/skip/abort) — no TTY required
- [ ] (later polish) Glamour markdown + Chroma syntax highlighting for explanations

### Phase 3 — Diagnosis engine ✅
- [x] `core/diagnose`: structured parsers — exit-code meanings + recognized patterns
- [x] Parsers: port-in-use, perms, ENOENT, conn-refused, DNS, disk-full, TLS, systemd, pkg-mgr, k8s (ImagePull/CrashLoop/OOM/RBAC/NotFound), Python/Go/Java/Node stacks
- [x] Signals printed AND fed into the diagnosis prompt; fix re-enters the approval gate

### Phase 4 — Knowledge packs / RAG ✅
- [x] `core/vector` brute-force cosine + `core/embed` (Embedder iface: HashEmbedder offline default, Ollama embedder)
- [x] `.sahayakpack` format (single file, pre-embedded, model-pinned manifest, integrity hash)
- [x] `knowledge install/list/search/build/remove`; sample k8s pack source in `docs/packs/`
- [x] Hybrid retrieval (vector + keyword, RRF) injected into the planner prompt
- [ ] (spec target, later) swap store to `sqlite-vec`/wazero + `bge-small` ONNX embeddings

### Phase 5 — Memory (deep-agent state) ✅
- [x] `core/memory`: long-term namespaced memories with vector recall + atomic-write persistence
- [x] Session checkpoints (save/load/clear) for resume/crash-safety
- [x] `memory add/list/search/forget`; recall injected into planning, requests auto-remembered
- [ ] (later) `write_todos` planning tool + `spawn_subagent` isolated-context diagnosis; resume UX

### Phase 6 — The appliance (embedded inference) ✅
- [x] `core/llm.Embedded`: real llama-server lifecycle — fixed-port→ephemeral fallback, atomic port-file, `/health` gating, warm reuse, `Stop()`
- [x] OpenAI-compatible chat (`/v1/chat/completions`, `json_object` for structured output)
- [x] `--engine embedded|ollama`; clear actionable error until the binary+GGUF are bundled
- [ ] (release-time) `go:embed` the prebuilt `llama-server` + Granite GGUF; per-OS appliance pipeline

### Phase 7 — Hardening & release ✅
- [x] Fuzz test for the plan parser (400k+ execs, no panics); compile-time interface assertions
- [x] `.goreleaser.yaml` (multi-arch, CGO-free, SBOM, checksums); `NOTICE.md`; GitHub Actions CI (+ cross-compile matrix)
- [x] `sahayak doctor` reports engine/embedder/packs/memory/backend
- [ ] (later) cosign signing wired; shell completions; man pages; docs site

### Beyond v1 (deferred)
OpenAI-compatible adapter (vLLM/TGI/LM Studio) · remote SSH executor (fleets) ·
native Kubernetes (client-go) · cross-encoder reranker · pure-Go ANN (`coder/hnsw`)
for >500k-chunk packs · optional cloud escape-hatch adapter (off by default).

---

## 6. Key risks & mitigations

| Risk | Mitigation |
|---|---|
| **Embedded model license** contaminates the "sovereign" promise | Apache-2.0/MIT-only rule for embedded weights; Granite default; Gemma as adapter-only; `THIRD_PARTY_LICENSES`. |
| **Per-OS/arch native artifacts** (not one universal binary) | Accept it (inherent to native inference); GoReleaser matrix; ship GGUF as sibling file to keep binary lean. |
| **cgo cross-compile pain** | Avoided: embedded `llama-server` subprocess + pure-Go SQLite + WASM `sqlite-vec` + pure-Go embeddings keep `CGO_ENABLED=0`. |
| **Weak small model → bad commands** | Mandatory approval gate; risk tiers; RAG grounding; GBNF-guaranteed structure; Ollama upgrade path. |
| **Dangerous command executed** | Structured args (no `sh -c`); destructive tier always gated; dry-run preview. |
| **Trust** — users must believe nothing leaks | Zero telemetry default; redaction; auditable loop; signed releases + SBOM. |
| **Eino pre-1.0 churn** | Kept behind a swappable interface; bespoke loop is the primary path. |
| **RAG scale ceiling** (`sqlite-vec` brute-force) | Fine to ~100k chunks/pack; int8 quant to ~500k; `coder/hnsw` ANN if ever needed. |

---

## 7. Conventions
- Module path `github.com/ZentienceLabs/sahayak-cli`; binary `sahayak`.
- Conventional commits (`feat:`, `fix:`, `chore:`), atomic commits.
- Explicit return types on exported funcs; named exports favored; strict typing.

---

## 8. Research provenance (June 2026)
Stack decisions are backed by current research on: optimal open-weight models + licensing
for embedded weights; Go embedded-inference approaches (and Ollama's 2026 shift to
`llama-server` subprocess); reconciling "LangGraph deep agents" with a Go single binary
(Eino, bespoke loop, SQLite memory); and cgo-free embedded vector DB + knowledge-pack
design (`sqlite-vec`/wazero, `bge-small` ONNX, `.sahayakpack`). See team notes for sources.

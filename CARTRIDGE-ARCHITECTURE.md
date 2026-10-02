# Sahayak — Cartridge Architecture (the generalization plan)

> The plan to make Sahayak work for **all tools** (k8s, az, PowerShell, docker, systemd…)
> without writing Go per tool — and to let it **self-improve** safely. This supersedes the
> implicit "k8s tool" shape; `ARCHITECTURE.md` (division of labor, why-deterministic) still
> holds and this builds on it.

## The premise (restated, canonical)

Sahayak = **an LLM that understands the user + swappable knowledge cartridges + deterministic
guards.** A *cartridge* is a self-contained body of knowledge for one tool/domain. You add a
tool by **dropping in a cartridge**, not by writing code. The model understands the request and
retrieves the right knowledge; deterministic Go keeps the dangerous parts safe.

The earlier hardcoded-Go playbooks were a reliability detour: they turned *knowledge* ("how to
debug k8s logs") into *binary code*, which is why Sahayak became k8s-specific. The cartridge
model puts that knowledge back into data — where it scales.

## Three layers

| Layer | Where | Knows about a specific tool? | Responsibility |
|---|---|---|---|
| **Engine** | Go, in the binary | **No** | RAG retrieval, routing, slot extraction, template assembly, risk gate, dry-run, exec, redaction, the investigate loop, generic combiners, the cartridge loader, self-learning telemetry |
| **Brain** | local LLM | **No** | Understand the request (pick intent) + fill typed slots. **Never authors a command string.** |
| **Cartridge** | data (a `.sahayakpack`) | **Yes** | All tool specifics, as data: knowledge + catalog + command templates + applicability probe |

> The model understands; Go acts; the human authors the cartridge; the human approves mutations.

## What a cartridge contains

1. **Knowledge** (RAG chunks + pre-computed embeddings) — facts, gotchas, how-to scenarios.
   *The breadth mechanism: drop a doc, get a tool.* (Exists today as `.sahayakpack`.)
2. **Catalog** — example phrasings → intent (the `catalog.txt` format, per cartridge).
3. **Command templates** — `intent → command + typed slots + risk tier`. *Replaces the
   hardcoded Go playbooks.* Each slot has a name + an extraction rule (see slot engine).
4. **Applicability probe** — a cheap check that answers "am I relevant on this machine?"
   (e.g. k8s: a kubectl context exists; systemd: `systemctl` present; az: logged in). Used for
   peer disambiguation.
5. **Metadata** — name, version, embed-model id + dim (model-pin, exists).

### Two tiers of an op (the key move — scale incrementally)
| Tier | Mechanism | Reliability | Author effort |
|---|---|---|---|
| **Knowledge-only** (most ops) | RAG retrieves the how-to; LLM acts (read-only auto-runs, mutations gated) or advises | best-effort | drop a doc |
| **Templated** (risky/frequent few) | typed template → Go assembles the command deterministically | rock-solid | author a template |

A new tool ships **day one as knowledge-only** (instant breadth, zero code); templates are
added *only* for its dangerous/common ops over time.

## Peer cartridges + disambiguation (no primary/secondary)

All installed cartridges are **peers**. The engine keeps **one unified cross-cartridge index**
(catalog + RAG corpus + template registry, each entry tagged with its cartridge). A request is
matched globally; the winning entry's cartridge runs. Selection is **per-request by relevance**,
never a fixed rank. When several cartridges could match ("restart nginx" → systemd? k8s? docker?),
resolve deterministically, in order:

1. **Applicability probe** — prune cartridges whose tool isn't present/relevant here.
2. **Grounding** — the candidate template's slots must actually fill from the request.
3. **Score margin** — clear winner → use it; near-tie → **ask** (honest fallback), never guess.
4. **Explicit hints** — "the *service*" / "the *pod*" / "the *container*" route themselves.

Intents/templates are **cartridge-namespaced** (`k8s.restart` vs `systemd.restart`), so names
never clash. Cross-cartridge **composition** (e.g. "AKS pods failing → check Azure quota" =
k8s + az) is the composition layer spanning cartridges — possible because they're peers; deferred.

## Self-learning = assisted authoring from deterministic signals

Goal: Sahayak learns which scenarios work and which don't, and gets better. The **safe** form
(the only one that survives a weak CPU model):

- **The judge of success is DETERMINISTIC, never the model:** exit code, dry-run pass/fail,
  did-the-user-approve, did-the-user-immediately-run-a-corrected-command.
- **The model may DRAFT a change; the human APPROVES it.** No silent self-modification.
  *(We already got burned by the model-judged version — auto-remembered conclusions poisoned
  future runs and had to be disabled. Do not repeat it.)*
- **Learning lands in the cartridge (data), not the weights.** No runtime training.

What it produces (each = a proposed cartridge edit the human accepts/rejects):
- An ad-hoc command that succeeds repeatedly → **draft a candidate template** ("promote to a playbook?").
- A template that fails repeatedly → **flag "needs fixing"** with the failing cases.
- A phrasing that routed wrong but the user corrected → **suggest adding it to the catalog.**
- Per-template success-rate **telemetry** → low-confidence templates get extra guarding.

This is the existing **curator + envfacts** pattern (deterministic extraction, TTL'd,
self-invalidating, no model judgment) extended from "stable facts" to "skills."

## Tech stack (sovereign, CPU, Go, `CGO_ENABLED=0`)

| Concern | Choice | Notes |
|---|---|---|
| Language / dist | Go single static binary, CGO-free | unchanged |
| Brain — route/extract | **qwen3:4b-instruct** (Apache) | independently validated for tool-calling |
| Brain — planning (optional) | **phi-4-mini-instruct** (MIT) via `SAHAYAK_PLAN_MODEL` | same size, cleaner CoT; opt-in |
| Inference | embedded **llama.cpp/llama-server** (appliance) + **Ollama** (dev) | unchanged |
| Structured output | **GBNF / JSON-schema** | shipped (both lanes) |
| Embeddings | **nomic-embed-text** (semantic) / HashEmbedder (offline) | shipped, pluggable + model-pin |
| Reranker | **MMR** (model-free, default, shipped) → optional **mxbai-rerank-base-v2** (Apache, 0.5B) or **bge-reranker-v2-m3** GGUF via llama.cpp `/rerank` | reuses the inference lane; behind the `Reranker` interface |
| Vector store | current pure-Go stdlib store (fine at cartridge scale) | options if scale grows: **chromem-go** (pure Go, zero-dep, RAM-greedy) or **sqlite-vec** via ncruces/wazero (CGO-free) |
| Hybrid retrieval | vector + keyword RRF | shipped |
| Cartridge template format | **TOML** (pure-Go lib, human-authorable) or stdlib **JSON** (zero-dep) | catalog stays the `catalog.txt` line format |
| Slot engine (NEW Go) | named extractor primitives + grammar-constrained LLM fallback | the keystone — see below |
| Self-learning store | JSON telemetry in `~/.sahayak/` + background miner | extends curator/envfacts |

## What moves, what's new

- **Go → cartridge data:** the 7 k8s playbooks' intents, command strings, slot wiring, phrasings
  → `k8s.cartridge` (the reference cartridge).
- **Stays/grows in Go (engine):** risk gate, dry-run, exec, redaction, RAG, the loop, **+ new:
  cartridge loader, template runner, slot engine, combiner library, self-learning telemetry/miner.**
- **The keystone (hardest new piece): the slot engine.** Generalize today's Go extractors
  (`appEntity`, `envVarRe`, `resourceAlias`, `selectorEntity`, content-keyword) into a **library
  of named primitives** (`hyphenated-token`, `upper-snake`, `enum`, `after-preposition`,
  `content-keyword`) that templates reference declaratively; **fall back to grammar-constrained,
  grounded LLM slot-filling** when no primitive fits. Deterministic where we can, model where we
  must, safe either way.

## Reliability invariants (must hold for every cartridge)
1. The model **never** emits a command string — it picks a template and fills typed slots.
2. The **command shape is human-authored cartridge data**, assembled by Go.
3. Mutations are **risk-gated + dry-run-validated**; the human approves.
4. Self-learning's success-judge is **deterministic**; the human approves what's learned.
5. Genuine ambiguity → **ask**, never guess.

## Staged migration (each step shippable, k8s reliability guarded by existing tests)
1. **Cartridge format** — extend `.sahayakpack` to carry catalog + templates (+ slot specs +
   applicability) beside knowledge chunks.
2. **Engine** — template runner + slot engine (extract today's Go extractors into primitives) +
   risk-from-template + cross-cartridge unified index + disambiguation.
3. **Port k8s → `k8s.cartridge`** (reference). Existing playbook tests become the parity suite;
   retire the hardcoded Go once behavior matches.
4. **Second cartridge as pure data** (systemd or docker) — proves "new tool = no code."
5. **Self-learning layer** — telemetry + background miner drafting cartridge edits for approval.
6. **Cross-cartridge composition** (later) — recipes referencing template intents + combiners.

> Keystone is step 2 (slot engine); it's the single biggest piece and what makes "works for all
> tools" real. Steps 1–3 lose **no** k8s reliability (tests guard it) while converting Sahayak
> from "a k8s tool" into "a tool-agnostic engine + a k8s cartridge."

## Build status (2026-06-28)

The engine foundation + k8s-as-data are **built, tested, and live-verified**:

- ✅ **Slot engine** (`core/slots`) — named extractor primitives (hyphenated-token,
  upper-snake, after-preposition, content-keyword, enum) generalizing the k8s extractors.
  Tested.
- ✅ **Cartridge format + runner** (`core/cartridge`) — `Template.Build` grounds slots and
  assembles a command from data; `Parse` validates packs; reference `k8s.json` embedded.
  Tested.
- ✅ **Cross-cartridge index** (`core/cartridge/index.go`) — peer routing: embeds all
  cartridges' catalogs into one space, routes by meaning, grounds via the slot engine.
  Tested (hermetic, HashEmbedder).
- ✅ **Agent integration** (`core/agent/cartridge.go`) — `tryCartridge` runs "simple"
  templates fully from data (command + named processor: `filter-summarize`,
  `configmap-search`); "resolve-fanout" intents delegate to the existing runners for parity.
- ✅ **CLI wiring** — opt-in `SAHAYAK_CARTRIDGE=1`; `buildCartridges` in `setupAgent`.
- ✅ **LIVE-VERIFIED** against AKS: `list`, `searchcfg` (fully data-driven), and `image`
  (cartridge-routed) all ran from cartridge data — "cartridge k8s → list ≈ 60%" etc. 15
  packages pass, vet+gofmt clean.

- ✅ **Second tool as pure data** (`systemd.json`) — a full cartridge (status/restart/
  start/stop/logs/enable) with **zero new tool-specific Go**; only one reusable shared
  primitive (`after-verb`, for bare service names) was added to the engine. Peer routing
  verified: k8s phrasings → k8s, systemd phrasings → systemd.
- ✅ **Applicability + peer disambiguation** — each cartridge's probe runs once/session;
  the index prunes tools not present on the host (no primary/secondary). LIVE: on a
  Windows host with kubectl but no systemctl, systemd is pruned ("not applicable here —
  skipping it") and k8s routes/runs; "restart the nginx service" does not mis-route.

- ✅ **Fanout as data** — `resolve-fanout` templates carry a per-item command (`item`);
  the engine resolves the set, substitutes `{name}/{ns}/slots`, runs each, and applies a
  per-item processor (`raw`/`error-extract`). image/rollout/restart/verifyenv/logs are now
  **pure data** — no per-intent Go. Status rollup is a `rollup`-shape template.
- ✅ **Cartridge engine is the DEFAULT** — legacy (regex playbooks + semantic router +
  classifier) is unwired into `legacyRoute`, opt-in only via `SAHAYAK_LEGACY=1`. When
  cartridges are on, they are the sole deterministic router; a miss → investigate loop.
- ✅ **Installable / marketplace** — `cartridge.LoadAll` = built-ins + `~/.sahayak/cartridges`;
  `cartridge.Install(path|https-url)` validates + installs; CLI `sahayak cartridge
  list|install|where`. LIVE: installed a new `docker` cartridge from a file → listed as a
  peer, zero code.

- ✅ **Self-learning layer** (`core/learn`) — observe-only, deterministic-signal,
  human-gated. Records routed outcomes / ad-hoc `!` commands / unmatched requests
  (judge = exit code & routing hit, never the model); `sahayak learn suggest` drafts
  promote-template (works → templatize), fix-template (fails → review), cover-gap
  (uncovered). Static base never auto-mutated; promotion = a human installing a cartridge.
  LIVE: repeated `! kubectl get ns` → promote-template suggestion. Tested.

- ✅ **KB-in-cartridge + markdown builder** — `Cartridge.Knowledge []string` (raw text,
  embedder-portable); `cartridge.Compose` + `ChunkMarkdown`; CLI `sahayak cartridge build
  --name X --kb doc.md [--templates t.json]`. At load, cartridge KB is embedded locally and
  fed to the retriever, so a tool ships **commands + knowledge together**. LIVE: built a
  `redis` cartridge from a markdown how-to (3 KB chunks) + a template. Tested.

**Recommended marketplace design (sovereign-appropriate):** a **static, signed registry
index** — NOT a hosted service. The index is a JSON file (name, version, tool, description,
URL, checksum, signature) hostable on Git raw / blob storage / an internal server / a local
file. Client: `cartridge registry add <url|path>` (multiple sources — official + private),
`cartridge search`, `cartridge install <name>` (resolve via index → download → verify
checksum/signature → **show declared commands+risk for human review** → install),
`cartridge update`. Works connected AND air-gapped (mirror the index+files in); files not a
service (auditable, no live dependency); signed + review-on-install because cartridges carry
executable commands (supply-chain safety). This is "npm-for-cartridges, the sovereign way."

- ✅ **Marketplace registry** — static JSON index (GitHub-raw/blob/file hostable),
  separate `registry/` repo scaffold (index + cartridges + README). `cartridge registry
  add|list`, `cartridge search`, `cartridge install <name>` (resolve → download). Multiple
  registries are peers (official + private).
- ✅ **Supply-chain trust (closed)** — INTEGRITY via SHA-256 checksum + AUTHENTICITY via
  stdlib **ed25519 signatures** verified against locally-trusted keys; install shows the
  cartridge's declared commands+risk for review. `cartridge keygen | sign | trust`. LIVE:
  verified-install, untrusted-signer refused, tampered-bytes refused. `cartridge remove` too.

- ✅ **Auto-promotion (human-gated)** — `sahayak learn promote --intent X --phrase "a,b"`
  turns a learned successful command into a routable template in a dynamic-OVERLAY cartridge
  (static base untouched; risk auto-classified). LIVE: `! kubectl get ns` → promote →
  `ask "show all namespaces"` routes to the learned intent.
- ✅ **Bundling** — a cartridge JSON already IS the single signed bundle (commands + KB
  text, embedder-portable); `cartridge build --from-pack <name>` folds an existing knowledge
  pack into a cartridge, bridging the old `.sahayakpack` knowledge into the new model.

**Status: the cartridge architecture is feature-complete.** Engine (slots · templates ·
peer index · fanout · rollup) · tools-as-data · build from markdown/pack · install from
file/URL/registry-by-name · ed25519 signatures + checksum + applicability · self-learning ·
human-gated promotion · remove/trust/keygen/sign. Legacy is opt-in (`SAHAYAK_LEGACY=1`).
The legacy Go playbooks remain in-tree behind `SAHAYAK_LEGACY=1` (the existing tests still
guard them) but are no longer the default.

## See also
- `ARCHITECTURE.md` — division of labor, why-deterministic, model choice.
- `COVERAGE.md` — the op checklist (becomes per-cartridge).
- Memory `sahayak-cartridge-architecture`, `sahayak-intelligence-roadmap`.

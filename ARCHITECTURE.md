# Sahayak — Canonical Architecture

> The single reference for *who does what* in Sahayak. When a design question
> comes up, answer it against this doc. The whole point is that **every
> responsibility has exactly one correct home** — when work drifts into the
> wrong home, the system gets unreliable and development starts to feel like
> circles.

## The core thesis

Sahayak is a **sovereign, air-gapped DevOps CLI**: plain-language requests →
inspectable, approved commands against a cluster, running as a single static Go
binary on CPU with no GPU and no network.

A small local model (4B class, e.g. `qwen3:4b-instruct`) **cannot** plan and
author ops commands the way a frontier model (Claude / GPT-5.5) can. That gap is
**capability, not tuning** — no prompt, few-shot, agent loop, or fine-tune closes
it air-gapped on CPU. Proven repeatedly with our own evals: on a playbook-covered
task the model is bypassed and output is **byte-for-byte identical** across
models; the playbook-vs-no-playbook gap dwarfs the model-vs-model gap.

**Therefore: put the intelligence in the SYSTEM, not the model.** Make the model
do as little as possible. Push every decision that can be made correctly into
deterministic Go, captured human knowledge, or an honest "I don't know."

## Division of labor — the one table that matters

| Responsibility | Owner | Why it lives there |
|---|---|---|
| **Route** — which procedure does the user mean? | **SLM** (+ deterministic guards) | Fuzzy phrasing → bounded intent. Verifiable, low-risk. |
| **Extract** — fill the slots (app, env var, keyword) | **SLM** (+ grounding guards) | Pull entities from free text. Guarded so a wrong slot is rejected, not run. |
| **Narrate** — turn command output into a readable answer | **SLM** | The one job it's genuinely good at. |
| **Novel planning** — *how* to perform an op | **Playbook** (code/data) | A verified procedure runs identically every time. The model never authors a command. |
| **Authoring a new procedure** | **Human** (once, as data) | When no playbook exists, a person who knows the op writes it down once → it's a playbook forever. |
| **Command construction** | **Go** | Typed slots → structured `os/exec` args. A garbled slot fails before it reaches the shell. |
| **Validation** — is this command valid & safe? | **Go** | Deterministic critic: schema, whitelist, risk rules, dry-run. *Never* the model judging itself. |
| **Approval on mutations** | **Human** | Final yes/no on anything destructive (the approval gate / `!`). |
| **Truly unknown request** | **Human** (honest fallback) | "No verified procedure — closest is X, or run it yourself via `!`." Never improvise a dangerous command. |

The SLM occupies only the narrow middle — **understand**. Go **acts**. The human
**authors and approves**. Read it as a sentence:

> **The model understands; Go acts; the human authors and approves.**

## Why each "fix" lives where it does

Three things are permanently outside what *any* 4B model (even fine-tuned) can do.
Each has a real, non-model fix:

1. **Novel planning** → finite **catalog of playbooks** (the real ops a team runs
   are countable, ~30–50, not infinite) + **human authors** the rare new one as
   data + **honest fallback** for the true long tail.
2. **Knowing when it's wrong** → **deterministic critic** (schema / whitelist /
   risk rules / `--dry-run=server` / `auth can-i`) and a **propose→validate→repair
   loop where Go is the judge, not the model**. Confidence gating: if grounding
   guards fail, *refuse* — don't guess.
3. **Garbling commands** → the model **never emits a command string**; it fills
   typed slots and **Go assembles** with structured args. Whitelist of shapes +
   risk classification (ReadOnly auto-runs, Mutating needs approval + dry-run).

The unifying principle:

> Everything fine-tuning can't fix is fixed by moving the responsibility **out of
> the model's weights** and into one of three places: **deterministic Go**,
> **human knowledge captured as data**, or **an honest "I don't know."**

## Loops: who holds the judgment

A self-reflecting ReAct / "plan and improve" loop does **not** bridge the gap. A
loop is a *multiplier* of per-step reliability, not an *adder* of capability:
frontier models converge (0.95/step compounds up); a 4B diverges (0.5/step
compounds down) and its "reflect" step needs the very capability that's missing —
judging a command is as hard as writing it. We saw self-correction turn a correct
"no errors" into a wrong "found 6 matches."

The **only** loop that helps a small model:

> **Model proposes, Go disposes.** Model emits a candidate → Go validates against
> schema/whitelist/risk/dry-run → on failure, reject with a *precise* error → model
> retries. The critic is deterministic, so the feedback signal is real.

Keep judgment in Go and a loop is an asset; put judgment in the 4B and a loop is a
liability.

## Fine-tuning — what it does and does not buy us

**Verdict: not the highest-leverage move, and never the path to "smart enough to
plan."** Fine-tuning (LoRA on the Apache-2.0 base, merged GGUF, license-clean to
ship; one-time cloud-GPU training, runtime stays CPU-only/sovereign) is a
*redistribution* of the capability a 4B already has toward our domain — it does
not add capability.

**What fine-tuning CAN improve** (all in the narrow "understand" middle):

- **Phrasing generalization on routing** — natural phrasings map to the right
  intent without a hand-written regex per phrasing. This is the whack-a-mole
  killer, done in weights instead of `if`-statements.
- **Slot extraction robustness** — pulling `app` / env var / keyword across
  phrasing variety more reliably than regex.
- **Narration** — cleaner, more consistent readback of command output.

**What fine-tuning CANNOT fix** (and what to do instead — see table above):

- **Novel planning** for unseen tasks (generalizes poorly out-of-distribution) →
  *playbook catalog + human authoring + honest fallback*.
- **Knowing when it's wrong** (no self-evaluation) → *deterministic Go critic +
  dry-run*.
- **Safety / not garbling commands** → *typed slots + Go command construction +
  risk gating*.

**The trap to avoid:** do **not** fine-tune on `request → exact command` pairs to
"teach it your commands." That produces a *fuzzier, less reliable* version of the
deterministic catalog you already have — ~95–99% vs 100%, opaque vs inspectable,
re-train vs edit-one-line, can regress silently. Trading a thing that works
perfectly for a thing that works mostly is the circle in a new costume.

**Cheaper alternative that captures most of the win:** a **semantic / embedding
router** (embed the request, match to nearest intent example by cosine similarity)
gives most of the phrasing-generalization benefit with **no GPU, no training, full
inspectability, add-an-intent-by-adding-a-sentence** — CPU-only and sovereign.
Reach for fine-tuning only *after* a semantic router, and only if a measured gap
remains.

**Measured, not estimated** (2026-06-28, live against the catalog, 14 deliberately
*distant* phrasings chosen to share few keywords with the catalog — the hard cases):

| Embedder | Correctly routed | Notes |
|---|---|---|
| `hash:256` (offline, lexical) | **1/14 → 3/14** | declines (falls through) rather than mis-firing; only near-literal phrasings clear 0.45 |
| `nomic-embed-text` (semantic) | **11/14 → 13/14** | correct matches score 51–73%; the real unlock, still CPU-only |

(The `→` is after adding two restart phrasings to `catalog.txt` — the "add a line of
data, no code" fix in action.) The semantic embedder delivers ~11× the coverage on
hard phrasings for the price of one `ollama pull`. **Recommendation: enable
`nomic-embed-text` wherever an embedding model can ship; keep `hash` as the safe
air-gapped default (it guesses *less*, not more).** The remaining nomic miss was a
borderline below-threshold case, not a wrong route; the two former misses were novel
*restart* verbs ("kick", "force a fresh start") that mis-routed toward read-only
intents (unhelpful, never unsafe) and were fixed by adding catalog lines. Reproduce
with `SAHAYAK_COMPARE=1 go test ./core/router -run TestEmbedderCoverageComparison -v`.

| | Deterministic catalog | Semantic router | Fine-tune to emit commands |
|---|---|---|---|
| Reliability on covered tasks | 100% | routes, Go still acts → 100% | ~95–99%, can hallucinate |
| Add a case | edit one line of data | add one example sentence | re-train + re-eval |
| Inspect / audit | read the rule | read the examples | opaque weights |
| Regress silently | no | no | yes |
| Needs GPU | no | no | yes (one-time) |
| Sovereign / CPU-only | yes | yes | runtime yes, training no |

## What "done" means (so it stops feeling infinite)

Sahayak "works" when:

1. It **covers the ops the team actually runs** (finite — list them on a whiteboard).
2. It **understands how those ops are naturally phrased** (semantic router).
3. It **never does the wrong dangerous thing** (deterministic guards — already true).
4. It **reads cleanly** (narration — already decent).

The target is **"handle our ~40 ops, phrased naturally,"** not "handle anything."
The first has an edge you can walk to; the second has no finish line and is the
source of the circular feeling.

## Build status (all CPU-only, no GPU, no training)

All four shipped (2026-06-27/28):

1. **Semantic router + data-driven catalog** — `core/router`. Matches phrasings the
   regex matchers miss to `catalog.txt` examples BY MEANING, then `playbook.BuildPlan`
   grounds the slots. Add a phrasing by editing data (`catalog.txt` or `SAHAYAK_CATALOG`),
   not code. Embedder-pluggable (hash offline; `ollama:nomic-embed-text` for true
   semantics). Wired between the regex playbooks and the model classifier. ✅
2. **Honest fallback** — `honestNoConclusion`: when nothing grounds an answer, says so
   and points at a playbook phrasing or the `!` escape hatch; never guesses. The shell's
   `!` runs an operator command (still risk-gated). ✅
3. **Dry-run validation + repair** — `exec.DryRunArgs` + `agent.runValidated`: a kubectl
   mutation is validated with `--dry-run=server` first; rejection means the real command
   never runs, and in the loop the failure feeds back for the model to correct
   (propose → Go disposes → repair). ✅
4. **Fine-tune pipeline (OPTIONAL)** — `finetune/`: Modal QLoRA on an Apache-2.0 base for
   the router (route+extract), gentle recipe, **eval-against-base gate**. Off the
   critical path; ship only if it measurably beats base. ✅

Fine-tuning remains an optional optimization, not a dependency — the semantic router
delivers the "understands more phrasings" win without it.

### Reliability/intelligence upgrades (2026-06-28, all CPU-only, no training)

Built on the roadmap of "swap-behind-an-interface, measurable, sovereign" moves (memory
`sahayak-intelligence-roadmap`):

5. **Grammar-constrained decoding** — both lanes now constrain output at the DECODE
   boundary. `core/llm/schema.go` tightened (classifier `resource` enum + `env_var`
   pattern, mirroring the Go guards); the embedded appliance lane now forwards the JSON
   schema as an OpenAI `json_schema` (`core/llm/embedded.go`) so the shipped binary gets
   the same constraint as the Ollama dev lane, not a weaker `json_object`. Kills the
   malformed/semantic-garbage output class. ✅
6. **Real retrieval for knowledge packs** — `core/knowledge` gained a pluggable
   `Reranker` (model-free `MMRReranker` default: diversifies the candidate pool so
   near-duplicate runbook paragraphs don't crowd out a second relevant fact; a
   cross-encoder can drop in later behind the same interface) and **model-pinning**
   (`NewRetriever` warns + drops the vector arm when a pack was built with a different
   embedder than the query — no more silent keyword-only degradation). Set
   `SAHAYAK_EMBEDDER=ollama:nomic-embed-text` for true semantics. ✅
7. **Composition layer** — `core/playbook/composite.go` + `core/agent/composite.go`: a
   higher-level intent that runs SEVERAL atomic plays and synthesizes one verdict (today:
   a status rollup = image + rollout + **pod health** + recent-errors → HEALTHY/DEGRADED
   per namespace, with a TL;DR line). The model picks *which* composite (a route); Go runs
   the parts and concludes — the "model understands, Go acts" division lifted to
   orchestration, so multi-step answers need no command authoring by the model. Composites
   are **semantically routed** too (a `status` intent in the router catalog), so paraphrases
   like "give me the full picture of X" reach the rollup instead of falling into the slow
   model loop. ✅

**Model choice (researched 2026-06-28, deferred):** our scenario is "on-device tool
calling." Independent benchmarks split it our way — `qwen3:4b-instruct` is best for
route/extract (keep it), `phi-4-mini-instruct` (3.8B, MIT) has cleaner multi-step
chain-of-thought for the investigate loop at the SAME size/speed. A "thinking" model is
disqualified (50–70% malformed structured output). Plan: keep one model by default;
add phi-4-mini as an OPTIONAL planning-only lane (`SAHAYAK_PLAN_MODEL`) only if
deterministic coverage (composition + more playbooks) leaves a measured planning gap.

## See also

- `project.md` — full product spec.
- `CARTRIDGE-ARCHITECTURE.md` — the generalization plan: cartridges (knowledge packs) as the unit, peer disambiguation, self-learning, tech stack. **The forward direction.**
- `COVERAGE.md` — the op-coverage checklist (how close v1 is to "done").
- `RUNBOOK.md` — operational usage, playbooks, interactive shell.
- Memory `sahayak-architecture-decisions` — locked tech-stack decisions + session log.
- Memory `sahayak-division-of-labor` — the condensed form of this doc.
- Memory `sahayak-model-licensing-rule` — embeddable weights policy.

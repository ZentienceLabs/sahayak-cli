# Sahayak — Run Book

How to start and use Sahayak from a Windows terminal. Two terminals: one for the
model backend (Ollama), one for Sahayak.

---

## Terminal 1 — start the model backend (leave running)

Ollama is the dev brain. Start its server and keep this terminal open.

```powershell
# 1. Start the Ollama server (blocks — keep this terminal open)
%USERPROFILE%\ollama\ollama.exe serve
```

First time only — pull a model (run in a third terminal, or stop serve, pull, restart):

```powershell
# Sahayak's DEFAULT dev model (Apache-2.0, best small-model performer in our eval):
%USERPROFILE%\ollama\ollama.exe pull qwen3:4b-instruct

# Recommended balance of quality/speed on CPU (Apache-2.0):
%USERPROFILE%\ollama\ollama.exe pull qwen2.5-coder:7b

# Faster, lighter (good once the deterministic guards carry the load):
%USERPROFILE%\ollama\ollama.exe pull qwen2.5-coder:3b

# List what you already have:
%USERPROFILE%\ollama\ollama.exe list

# RECOMMENDED — the semantic-router embedder. Measured ~11× the phrasing coverage of
# the offline default (13/14 vs 1/14 on hard phrasings). Tiny, CPU-only, sovereign:
%USERPROFILE%\ollama\ollama.exe pull nomic-embed-text
```

> **Turn on semantic routing.** After pulling it, set
> `SAHAYAK_EMBEDDER=ollama:nomic-embed-text` (see config table below). This is the
> single highest-leverage setting for "understands how I phrase things." The default
> `hash` embedder is safe but nearly blind on novel phrasings — it *declines* rather
> than guessing, so without this you fall back to the model loop more often.

> If you don't set `SAHAYAK_MODEL`, Sahayak uses **`qwen3:4b-instruct`** against
> **`http://127.0.0.1:11434`** — the best-performing Apache-2.0 model in our eval —
> so either pull that, or set `SAHAYAK_MODEL` to whatever you pulled. (The embedded
> appliance still ships IBM Granite 4.0-micro; this default only governs Ollama.)

> Tip: in the Claude Code prompt you can run any of these by prefixing with `!`,
> e.g. `! %USERPROFILE%\ollama\ollama.exe serve` — output lands in the session.

---

## Terminal 2 — build & run Sahayak

```powershell
# Go to the project
cd path/to/sahayak-cli

# Build the single static binary (CGO-free)
go build -o bin\sahayak.exe .\cmd\sahayak

# Pick your model for this terminal (optional; defaults otherwise)
$env:SAHAYAK_MODEL = "qwen2.5-coder:7b"

# Health check — confirms backend + shows packs/memory/env-cache state
bin\sahayak.exe doctor
```

If `doctor` shows `backend: ✓ reachable`, you're ready.

---

## Interactive shell (pick a model, then just type)

```powershell
# Just run the exe — it lists your installed models, you pick one, then type requests
# in a loop (no need to re-run `ask` each time). One session = one warm agent + the
# background curator learning between your questions.
bin\sahayak.exe                 # or: bin\sahayak.exe shell

# See installed models without entering the shell:
bin\sahayak.exe models
```

Inside the shell:
```
⬡ Sahayak interactive shell  ·  model: qwen3:4b-instruct
sahayak> list configmaps for acme-web
sahayak> why is acme-web failing
sahayak> ! kubectl get ns   # escape hatch: run a command yourself (still risk-gated)
sahayak> models          # re-pick the model mid-session
sahayak> help            # built-in commands
sahayak> exit            # or quit / Ctrl-D
```

---

## Everyday use (one-shot)

```powershell
# Ask in plain language — defaults to the adaptive investigate loop.
bin\sahayak.exe ask "is there any errors in acme dev applications"
bin\sahayak.exe ask "what image is the acme worker deployment running"
bin\sahayak.exe ask "why did my pod crash in acme-dev"

# One-shot plan mode (model proposes the whole plan up front):
bin\sahayak.exe ask --plan "reload nginx after editing the server block"

# Bound the investigation steps (default 8):
bin\sahayak.exe ask --max-steps 5 "list failing pods across the cluster"

# Plain line-mode gate instead of the rich TUI (also auto on non-TTY):
bin\sahayak.exe ask --no-tui "show node pressure conditions"
```

### Recognized requests run deterministically (no model guesswork)
Common asks are handled by Go **playbooks**, not the model — so they never loop or
wrongly conclude. Seven intents: **list**, **logs**, **image**, **rollout**,
**restart** (mutating), **verifyenv** (mutating), **searchcfg**. Examples:

- **List** — `list configmaps for acme-web`: ONE read-only `kubectl get <res> -A`,
  filtered in Go, every match with its namespace (or a trustworthy "none anywhere").
- **Logs** — `why is acme-web failing`: resolves the deployment(s), reads each one's
  last 3h (`--all-containers --since=3h --tail=500`, ≤3 deployments), extracts the
  distinct errors. Add a hint (`…for github oauth`) to narrow.
- **Image / rollout** — `what image is acme-web running`, `is acme-web rolled out`.
- **searchcfg** — `is there a config flag for workflow`: greps configmap CONTENTS in Go.
- **restart / verifyenv** — mutating → approval-gated.

**Composite questions** get a synthesized answer: `how is acme-web doing` runs image +
rollout + a recent-error scan across every matched deployment and concludes a
**HEALTHY / DEGRADED** verdict per namespace — several playbooks composed in Go, no model
planning. Other rollup phrasings: `health of <app>`, `is <app> healthy`, `summarize <app>`.

**The routing pipeline (most-precise first, model does as little as possible):**
1. **Regex playbooks** — exact, instant, no model.
1b. **Composition layer** — recognizes a rollup intent ("how is <app> doing") and runs
   several atomic playbooks, combining them into one verdict. Still no model planning.
2. **Semantic router** (`core/router`) — matches phrasings the regex missed to a
   catalog of intents BY MEANING, then runs the same deterministic plan. Add a phrasing
   by editing `core/router/catalog.txt` (data, not code) or a `SAHAYAK_CATALOG` file —
   no recompile of a regex. Quality scales with the embedder (set
   `SAHAYAK_EMBEDDER=ollama:nomic-embed-text` for true semantics; hash is lexical).
3. **Model classifier** — one small, schema-constrained call, validated + grounded.
4. **Adaptive investigate loop** — for genuinely novel/pod-level questions.
5. **Honest fallback** — if nothing grounds an answer, Sahayak says so plainly and
   points at a playbook phrasing or the `!` escape hatch. It never guesses a command.

**Mutations are validated before they run:** a `kubectl` mutation that supports it gets
a server-side `--dry-run=server` first; if the cluster rejects it, the real command is
NOT executed and you see the precise API error (model proposes, Go disposes).

### What you'll see
- A live **token stream** with a counter while the model thinks (not a dead spinner).
- Each command **explained**, with a **risk tier** (read-only / mutating / destructive).
- Read-only commands auto-run; anything mutating waits for your **approval**.
- `remembered N stable fact(s)…` when it caches durable topology (namespaces,
  deployments). Run the same question again — the second run starts already knowing them.

---

## Environment & memory commands

```powershell
# Long-term notes you curate (these DO influence future runs):
bin\sahayak.exe memory add "prod cluster is AKS, 6 nodes, namespace prefix acme-"
bin\sahayak.exe memory list
bin\sahayak.exe memory search "cluster"
bin\sahayak.exe memory forget "AKS"

# Knowledge packs (offline RAG — e.g. kubectl/az docs):
bin\sahayak.exe knowledge list
bin\sahayak.exe knowledge search -k 5 "rollout restart"

# Install the Acme ops pack (hard-won Azure/cluster facts from the runbook):
bin\sahayak.exe knowledge build --name acme-ops --from C:\tmp\acme-ops-facts.md -o C:\tmp\acme-ops.sahayakpack --command az
bin\sahayak.exe knowledge install C:\tmp\acme-ops.sahayakpack
```

### Cartridges (the architecture — default routing)
Sahayak now routes through **cartridges**: per-tool data packs (commands as templates +
slots + risk) matched by meaning across all installed tools (peers — no primary). This is
the **default**; the old regex/router/classifier path is opt-in via `SAHAYAK_LEGACY=1`.

```powershell
bin\sahayak.exe cartridge list                 # built-in (k8s, systemd) + installed
bin\sahayak.exe cartridge install C:\path\docker.json   # add a tool from a file…
bin\sahayak.exe cartridge install https://…/docker.json # …or a URL (the "marketplace")
bin\sahayak.exe cartridge where                # the install dir (~/.sahayak/cartridges)
```
A cartridge declares an **applicability probe**, so a tool not present on the host (e.g.
`systemctl` on Windows) is pruned and never mis-routes. For true-semantic routing set
`SAHAYAK_EMBEDDER=ollama:nomic-embed-text`. Tip: use a richer embedder for best matching.

### Self-learning (observe-only, you approve)
Sahayak watches **deterministic** signals (command exit codes, routing hits) and drafts
suggestions — it never changes behavior on its own.
```powershell
bin\sahayak.exe learn suggest   # promote (works→templatize) / fix (fails) / cover (gaps)
bin\sahayak.exe learn forget    # clear the learning log
```
Act on a suggestion by adding a phrasing/template to a cartridge and installing it — the
shipped (static) cartridges are never auto-modified.

### Where state lives (`%USERPROFILE%\.sahayak\`)
| File | What |
|---|---|
| `envfacts.json` | auto-learned topology cache (namespaces/deployments), TTL'd + self-invalidating |
| `memories.json` | your curated notes + machine-distilled `topology` notes |
| knowledge packs | installed `.sahayakpack` files |

To reset the auto-learned topology cache: delete `%USERPROFILE%\.sahayak\envfacts.json`.

---

## How background knowledge-building works (FYI)
While you read output or decide on an approval, a **background curator** uses that
idle CPU to consolidate what Sahayak learned about your environment. It shares ONE
inference gate with the foreground, and **always yields to your request** — so it
can never make the command in front of you slower. All local, nothing leaves your
network.

---

## Config knobs (env vars)
| Var | Default | Meaning |
|---|---|---|
| `SAHAYAK_MODEL` | `qwen3:4b-instruct` | Ollama model tag, e.g. `qwen2.5-coder:7b` |
| `SAHAYAK_ENDPOINT` | `http://127.0.0.1:11434` | Ollama endpoint |
| `SAHAYAK_ENGINE` | `ollama` | `ollama` (dev) or `embedded` (appliance) |
| `SAHAYAK_EMBEDDER` | `hash:256` | semantic-router/RAG embedder. **Set `ollama:nomic-embed-text`** for true semantics (~11× phrasing coverage); `hash` is the safe air-gapped fallback when no embedder can ship |
| `SAHAYAK_CATALOG` | _(none)_ | path to an extra intent catalog (same format as `core/router/catalog.txt`) layered on the built-ins |

Flags override env: `--model`, `--endpoint`, `--engine`.

---

## Optional: fine-tuning the router

Everything above is deterministic Go + a semantic router — **no GPU, no training.** If
you want to push routing further, `finetune/` has a one-command Modal pipeline that
fine-tunes a small Apache-2.0 model to be the intent router (route + extract), with a
**mandatory eval-against-base gate** (your prior router fine-tune scored *below* base, so
this is opt-in and only worth shipping if it measurably wins). See `finetune/README.md`.
The architecture and the model/Go/human division of labor are documented in
`ARCHITECTURE.md`.

---

## Quick troubleshooting
| Symptom | Fix |
|---|---|
| `backend: ✗ ...` | Start Ollama (Terminal 1) and pull a model |
| Slow first response | CPU prefill on a cold model; subsequent steps reuse context |
| Wrong/old namespace reused | `del %USERPROFILE%\.sahayak\envfacts.json` to clear the cache |
| Weak answers / blank reasons | Use a larger model (`qwen2.5-coder:7b`) |

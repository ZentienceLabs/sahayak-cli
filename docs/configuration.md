# Configuration

Sahayak is configured by **environment variables** (persist across runs) and **flags**
(per-invocation, override env). Flags win over env; env wins over built-in defaults.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `SAHAYAK_MODEL` | `qwen3:4b-instruct` | Model tag the brain uses (e.g. `qwen2.5-coder:7b`). |
| `SAHAYAK_ENDPOINT` | `http://127.0.0.1:11434` | Ollama endpoint. |
| `SAHAYAK_ENGINE` | `ollama` | Brain: `ollama` (dev), `embedded` (appliance), or `cloud` (hosted — **not sovereign**, see below). |
| `SAHAYAK_CLOUD_PROVIDER` | `anthropic` | Backend for the `cloud` engine. Today: `anthropic` (Claude). |
| `ANTHROPIC_API_KEY` | _(none)_ | API key for the `cloud` engine when the provider is `anthropic`. |
| `ANTHROPIC_BASE_URL` | `https://api.anthropic.com` | Override the Claude API root (e.g. a gateway). |
| `SAHAYAK_EMBEDDER` | `hash:256` | Embedder for routing + RAG. `hash:<dim>` (offline, lexical) or `ollama:<model>` (true semantics, e.g. `ollama:nomic-embed-text`). |
| `SAHAYAK_CATALOG` | _(none)_ | Path to an extra router catalog file, layered on the built-ins (legacy router path). |
| `SAHAYAK_LEGACY` | _(unset)_ | `1` = use the legacy regex/router/classifier pipeline instead of the default cartridge engine (for comparison). |
| `SAHAYAK_REQUIRE_SIGNED` | _(unset)_ | `1` = refuse to install a cartridge that isn't signed by a trusted key. |
| `SAHAYAK_LLAMA_SERVER` | _(none)_ | Path to the `llama-server` binary (embedded engine). |
| `SAHAYAK_MODEL_PATH` | _(none)_ | Path to the model GGUF (embedded engine). |

### The single most impactful setting
```sh
export SAHAYAK_EMBEDDER=ollama:nomic-embed-text
```
This switches routing/RAG from lexical (`hash`) to true semantic matching — measured ~11×
phrasing coverage on hard requests. Needs `ollama pull nomic-embed-text`.

### The `cloud` engine (hosted models — ⚠️ not sovereign)
Sahayak's whole point is staying on your machine: `ollama` (dev) and `embedded` (the sealed
appliance) keep every request local. The **`cloud`** engine is the opposite trade — an
explicit opt-in to a frontier hosted model when you want maximum reasoning during
development and sovereignty isn't the constraint for that session.

```sh
export SAHAYAK_ENGINE=cloud            # the hosted lane (off by default)
export SAHAYAK_CLOUD_PROVIDER=anthropic # default; Claude
export ANTHROPIC_API_KEY=sk-ant-...    # required
export SAHAYAK_MODEL=claude-opus-4-8   # any Claude model id
sahayak doctor                          # confirms the key + model are reachable
```

**What "not sovereign" means here:** with `cloud`, your prompts — plus the command output
the agent loop feeds back and the env facts it has learned — are sent over the network to
the hosted provider, where they may be processed under that provider's terms. Do **not**
use it on an air-gapped host or where data must not leave the machine; use `ollama` or
`embedded` for those. The model still never authors a command string or decides risk —
deterministic Go does — and the human approval gate is unchanged. The cloud engine is for
chat/inference only; semantic routing/RAG embeddings still come from `SAHAYAK_EMBEDDER`
(keep that on `hash` or a local Ollama embedder to avoid a second network dependency).

## Flags (apply to `ask` and `doctor`)

| Flag | Default | Meaning |
|---|---|---|
| `--engine <name>` | `ollama` | `ollama`, `embedded`, or `cloud` (= `SAHAYAK_ENGINE`). |
| `--endpoint <url>` | `http://127.0.0.1:11434` | Inference endpoint (= `SAHAYAK_ENDPOINT`). |
| `--model <tag>` | `qwen3:4b-instruct` | Model to use (= `SAHAYAK_MODEL`). |

### `ask`-only flags
| Flag | Default | Meaning |
|---|---|---|
| `--approve-all-readonly` | `true` | Auto-run read-only steps without prompting. Set `=false` to confirm even reads. |
| `--no-tui` | `false` | Use the plain line-mode approval gate instead of the rich TUI (auto on non-TTY). |
| `--investigate` | `false` | Force the iterative discover-step-by-step loop (this is already the default path). |
| `--plan` | `false` | Force one-shot plan mode (model proposes the whole plan up front). |
| `--max-steps <n>` | `8` | Bound the investigate loop's steps. |

### `shell` flags
`--endpoint`, `--model`, `--engine` (same meanings). Everything else is chosen interactively
or via env.

## Where state lives (`$HOME/.sahayak/`)

| Path | What |
|---|---|
| `envfacts.json` | Auto-learned topology cache (namespaces/deployments), TTL'd + self-invalidating. |
| `memories.json` | Your curated notes + machine-distilled topology notes. |
| `packs/` | Installed knowledge packs (`.sahayakpack`). |
| `cartridges/` | Installed tool cartridges (`*.json`). |
| `registries.json` | Configured cartridge registry sources. |
| `trusted-keys.json` | Trusted cartridge-publisher public keys. |
| `learn.jsonl` | Self-learning observation log. |
| `keys/` | (If you publish) your cartridge signing keys — **keep the private key secret**. |

To reset the auto-learned topology cache: delete `$HOME/.sahayak/envfacts.json`.

## Examples

```sh
# point at a remote GPU box running Ollama, with a bigger model, for one call
SAHAYAK_ENDPOINT=http://gpu-box:11434 SAHAYAK_MODEL=qwen2.5-coder:7b \
  sahayak ask "why did my pod crash?"

# semantic routing + require signed cartridges (hardened)
export SAHAYAK_EMBEDDER=ollama:nomic-embed-text
export SAHAYAK_REQUIRE_SIGNED=1

# compare the new cartridge engine vs the legacy pipeline
SAHAYAK_LEGACY=1 sahayak ask "list the configmaps for web-api"
```

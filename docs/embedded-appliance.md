# Embedded Appliance (sealed, air-gapped)

The default brain is Ollama (a separate daemon). The **embedded** engine instead runs a
bundled `llama-server` against a bundled GGUF model **inside Sahayak's own process tree** —
no Ollama, no network, fully sovereign. This is the mode for a shipped appliance or an
air-gapped box.

## How it works
On first use the embedded engine:
1. resolves the `llama-server` binary and a `*.gguf` model (see resolution order below),
2. launches `llama-server` on a managed loopback port (prefers **11923**, falls back to a
   free port; the chosen port is published atomically so a warm server is reused),
3. gates on `GET /health` (llama-server returns `503` while loading, `200` when ready),
4. talks to it over the OpenAI-compatible `/v1/chat/completions` (with JSON-schema
   structured output — the same grammar constraint as the dev lane),
5. keeps the warm server for the session and kills it on exit.

## What you must provide (not in the repo)
Two assets are multi-GB / per-platform, so they're **not** committed:
- the **`llama-server`** binary (from llama.cpp, built for your OS/arch),
- a **model GGUF** — **Apache-2.0 / MIT licensed only** (e.g. IBM **Granite 4.0-micro**,
  the embedded default target; Gemma is excluded by license).

## Wiring it up

### Option A — env vars (dev / quick)
```sh
export SAHAYAK_ENGINE=embedded
export SAHAYAK_LLAMA_SERVER=/path/to/llama-server      # or llama-server.exe on Windows
export SAHAYAK_MODEL_PATH=/path/to/granite-4.0-micro.gguf
sahayak doctor          # backend: ✓ reachable once the model loads
sahayak ask "show disk usage under /var"
```
Per-invocation instead of env: `sahayak --engine embedded ask "…"`.

### Option B — bundle in `assets/` (appliance layout)
Drop the assets next to the binary and no env vars are needed:
```
<dir-of-sahayak-binary>/
  sahayak(.exe)
  assets/
    llama-server/llama-server(.exe)
    models/your-model.gguf            # first *.gguf found is used
```

### Resolution order
- **llama-server:** `SAHAYAK_LLAMA_SERVER` → `assets/llama-server/llama-server[.exe]` next to the binary.
- **model GGUF:** explicit path → `SAHAYAK_MODEL_PATH` → first `*.gguf` under `assets/models/`.

If neither resolves, Sahayak prints a clear, actionable error (it does not fall back to the
network).

## Verify
```sh
SAHAYAK_ENGINE=embedded sahayak doctor
#   engine: embedded
#   backend: ✓ reachable        (first call pays model-load time, then it's warm)
```

## Notes
- **Air-gapped:** nothing leaves the box — inference, embeddings, knowledge, and learning are
  all local. Pair with offline knowledge packs / cartridges.
- **Licensing:** ship only Apache-2.0 / MIT weights in an appliance; see `project.md` §2. The
  source is MIT; bundled third-party weights/binaries keep their own licenses
  (`THIRD_PARTY_LICENSES` / `NOTICE.md` travel with released binaries).
- **CPU-only:** no GPU required at runtime; quantized small models (3–4B) are the target.
- **Embeddings:** the embedded engine serves chat; for semantic routing/RAG either keep the
  offline `hash` embedder or run a local embedding model and point `SAHAYAK_EMBEDDER` at it.

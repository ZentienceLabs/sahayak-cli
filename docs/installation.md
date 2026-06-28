# Installation

Sahayak is a single static Go binary (`CGO_ENABLED=0`). It needs a local **model backend**
to act as its brain — Ollama for development, or the embedded `llama-server` for a sealed
appliance.

## 1. Install the CLI

### With Go (recommended)
```sh
go install github.com/ZentienceLabs/sahayak-cli/cmd/sahayak@latest
# binary lands in $(go env GOPATH)/bin — make sure that's on your PATH
```

### From source
```sh
git clone https://github.com/ZentienceLabs/sahayak-cli
cd sahayak-cli
make build            # -> ./bin/sahayak   (CGO-free)
# or: go build -o bin/sahayak ./cmd/sahayak
```

Cross-compiles to linux / darwin / windows × amd64 / arm64 with no cgo.

### Verify
```sh
sahayak version
```

## 2. Install a model backend

### Option A — Ollama (development default)
```sh
# install Ollama from https://ollama.com, then:
ollama serve &                       # leave running
ollama pull qwen3:4b-instruct        # default brain (Apache-2.0)
ollama pull nomic-embed-text         # recommended: true-semantic routing/RAG
```

Other useful models:
```sh
ollama pull qwen2.5-coder:7b         # stronger on CPU if you have the RAM
ollama pull qwen2.5-coder:3b         # faster/lighter
ollama list                          # what you already have
```

If you don't set `SAHAYAK_MODEL`, Sahayak uses **`qwen3:4b-instruct`** at
**`http://127.0.0.1:11434`**.

### Option B — Embedded appliance (sealed, no Ollama)
The embedded engine runs a bundled `llama-server` against a bundled GGUF — fully
air-gapped. Those two assets are multi-GB and **not** in the repo. Provide them via:

```sh
export SAHAYAK_ENGINE=embedded
export SAHAYAK_LLAMA_SERVER=/path/to/llama-server
export SAHAYAK_MODEL_PATH=/path/to/model.gguf
```

(or drop them in `assets/`). Until present, Sahayak prints a clear, actionable error.
Embedded weights must be **Apache-2.0 / MIT** licensed.

## 3. Check everything is wired

```sh
sahayak doctor
```

`backend: ✓ reachable` means you're ready. If you see `backend: ✗ …`, start Ollama and
pull a model (or set the embedded env vars).

## 4. (Optional) add the cartridge registry

Tools beyond the built-in `k8s` and `systemd` are installed as cartridges:

```sh
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-registry/main/index.json
sahayak cartridge trust add fulEJgyF8DPnFR7cKcB00whsinJc2KXHMy692WfY/3M=
sahayak cartridge install k8s
```

See [Cartridges](./cartridges.md).

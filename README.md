# Sahayak (सहायक — "helper")

> **Sahayak is a sovereign, air-gapped AI command-line assistant for DevOps and Sysadmins.**
> It translates your plain-language requests into verified, safe operational commands.
> Unlike typical AI agents that unpredictably author commands on the fly, Sahayak uses a strict **"Model Understands, Go Acts, Human Approves"** architecture to guarantee safety, running entirely on infrastructure you control (CPU-only, no cloud dependency).

[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

## The Core Philosophy: Safety through Determinism

A small local model **can't** reliably author complex operational commands without making dangerous mistakes. Sahayak solves this by dividing the labor:
1. **The LLM understands:** It interprets your natural language, identifies the intent, and extracts the parameters (slots) like namespaces or app names.
2. **Go acts:** It takes the intent and slots, deterministically assembles the command from a pre-authored template (a "cartridge"), classifies the risk, and executes it.
3. **The human approves:** You write the cartridges and approve any mutating commands before they run.

## Cartridges: Tool Support as Data

Sahayak is tool-agnostic. You teach it how to use tools (like Kubernetes, systemd, or Redis) by installing **Cartridges**. Cartridges are data files containing curated knowledge (RAG), a catalog of phrasings, and command templates.

```sh
# Add the official registry, search, and install tools
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-cli/main/registry/index.json
sahayak cartridge install k8s
```

## Quick Start

```sh
go install github.com/ZentienceLabs/sahayak-cli/cmd/sahayak@latest

# Start your local model (e.g. Ollama)
ollama pull qwen3:4b-instruct       # default brain
ollama pull nomic-embed-text        # semantic routing

# Run Sahayak
sahayak doctor                                  # check backend + config
sahayak ask "how is web-api doing"              # composed health rollup
```

## Documentation Guide

Sahayak's documentation is divided into practical usage guides (in `docs/`) and core architecture references.

### 📚 Getting Started & Usage Guides
- **[Installation Guide](./docs/installation.md)**: How to set up Sahayak, build from source, and configure local model backends (Ollama or embedded).
- **[Configuration Guide](./docs/configuration.md)**: Detailed reference for environment variables, model tuning, and CLI setup.
- **[Commands & Options](./docs/commands.md)**: Comprehensive reference for all CLI commands, arguments, and interactive shell features.
- **[Cartridges Guide](./docs/cartridges.md)**: How to install, build, sign, and publish your own tool plugins to the registry.
- **[Self-Learning](./docs/self-learning.md)**: How Sahayak safely observes your terminal commands to suggest new templates without mutating its own behavior.
- **[Ops Teams Guide](./docs/ops-teams.md)**: Best practices for deploying Sahayak across an engineering team, sharing cartridges, and enforcing security.
- **[Embedded Appliance](./docs/embedded-appliance.md)**: Running Sahayak fully standalone with an embedded LLM.

### 🏗️ Architecture & Core Concepts
- **[ARCHITECTURE.md](./ARCHITECTURE.md)**: Explains the strict "Model Understands, Go Acts" division of labor and why Sahayak avoids unbounded AI agents.
- **[CARTRIDGE-ARCHITECTURE.md](./CARTRIDGE-ARCHITECTURE.md)**: Details the design of tool plugins, peer routing, and the data-driven engine.
- **[RUNBOOK.md](./RUNBOOK.md)**: Operational usage, playbooks, and interactive shell behaviors.
- **[project.md](./project.md)**: Product vision, roadmap, and design philosophy.
- **[COVERAGE.md](./COVERAGE.md)**: The current operational coverage checklist.

## Develop

```sh
make build   # ./bin/sahayak (CGO-free; cross-compiles to linux/darwin/windows × amd64/arm64)
make test    # 16 tested packages
make vet && make fmt
```

> Two release-time assets aren't in the repo (multi-GB, per-platform): the prebuilt
> `llama-server` binary and the embedded model GGUF. The embedded engine works the moment
> they're in `assets/` (or via `SAHAYAK_LLAMA_SERVER` / `SAHAYAK_MODEL_PATH`); until then a
> clear error points you at Ollama. Embedded weights ship **Apache-2.0 / MIT only**.

## License

[MIT](./LICENSE) © 2026 ZentienceLabs.

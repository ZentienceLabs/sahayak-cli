# Sahayak (सहायक / "helper")

> **Sahayak is a sovereign, air-gapped AI command-line assistant for DevOps and Sysadmins.**
> Tell it what's broken in plain English. It diagnoses the issue and gives you verified, safe operational commands to fix it.

[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

## The Ops Experience

No more hunting through StackOverflow at 3:00 AM or worrying about an AI agent deleting production. 

```sh
$ sahayak ask "why is the web-api pod crashing in the frontend namespace?"

🧠 Diagnosing... (Local CPU Model)

The pod is OOMKilled (Out of Memory). 
I have prepared the following safe command to inspect the previous logs:

> kubectl logs web-api-7b89d-xt4p -n frontend --previous | grep -i "memory"

[Execute]  [Edit]  [Cancel]
```

Unlike typical AI agents that hallucinate dangerous commands on the fly, Sahayak uses a strict **"Human Approves"** architecture. It runs 100% locally on your machine (no cloud required, no data leaks) and only executes pre-approved, safe command templates.

---

## 🚀 Quick Start for Users

### 1. Install Sahayak
Download the binary via Go:
```sh
go install github.com/ZentienceLabs/sahayak-cli/cmd/sahayak@latest
```

### 2. Start the Local AI
Sahayak uses [Ollama](https://ollama.com/) under the hood to run models securely on your local hardware:
```sh
ollama pull qwen3:4b-instruct       # The brain (CPU-friendly)
ollama pull nomic-embed-text        # Semantic routing
```

### 3. Ask a Question
Check that everything is wired up, then start troubleshooting!
```sh
sahayak doctor
sahayak ask "how is my local systemd docker service doing?"
```

---

## 🧩 Adding Tools (Cartridges)

Out of the box, Sahayak is a blank slate. You teach it how to manage your infrastructure by installing **Cartridges** (plugins for Kubernetes, AWS, Docker, Redis, etc.). 

Add the official registry and install the tools you use:
```sh
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-cli/main/registry/index.json

sahayak cartridge install k8s
sahayak cartridge install systemd
```

---

## 📚 Documentation

The `docs/` folder contains everything you need to deploy Sahayak across your engineering team:

- **[Installation Guide](./docs/installation.md)**: Setup, models, and dependencies.
- **[Commands & Options](./docs/commands.md)**: Interactive shell features and CLI arguments.
- **[Configuration Guide](./docs/configuration.md)**: Environment variables and tuning.
- **[Ops Teams Guide](./docs/ops-teams.md)**: Best practices for securely sharing cartridges across your team.

---

## 🛠️ For Developers & Architects

Sahayak solves the "AI safety" problem by dividing labor: **The LLM understands, Go acts, and Humans approve**. If you want to contribute, build custom internal cartridges, or embed Sahayak directly into air-gapped appliances without Ollama, check out our developer docs:

- **[Architecture Deep Dive](./ARCHITECTURE.md)**: Why we avoid unbounded AI agents.
- **[Cartridge Creation](./docs/cartridges.md)**: How to build, sign, and publish your own tool plugins.
- **[Embedded Appliance Mode](./docs/embedded-appliance.md)**: Running Sahayak fully standalone with a bundled C++ `llama-server`.
- **[Self-Learning Engine](./docs/self-learning.md)**: How Sahayak safely observes your terminal to suggest new templates.

### Building from Source
Sahayak is a pure, CGO-free Go binary.
```sh
make build   # Compiles to ./bin/sahayak (cross-compiles trivially)
make test    # 16 tested packages
```

## License

[MIT](./LICENSE) © 2026 ZentienceLabs.

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

### 2. Connect the AI Brain (Local or Hub-and-Spoke)
Sahayak uses [Ollama](https://ollama.com/) under the hood to run models securely on your own hardware. 

**Option A: Solo Setup (Local)**
Run the models directly on your laptop:
```sh
ollama pull qwen3:4b-instruct       # The brain (CPU-friendly)
ollama pull nomic-embed-text        # Semantic routing
```

**Option B: Team Setup (Hub-and-Spoke)**
If you are deploying to an Ops team, you don't want every engineer downloading 4GB models to their laptop. Instead, host one powerful Ollama server on your internal network (the "Hub"), and have your team connect to it (the "Spokes").

**1. On the Hub (Server):**
Install Ollama and configure it to accept network connections (by default, it only listens on localhost):
```sh
# Install Ollama
curl -fsSL https://ollama.com/install.sh | sh

# Start the server and expose it to the network
export OLLAMA_HOST="0.0.0.0:11434"
ollama serve

# Pull the required models
ollama pull qwen3:4b-instruct
ollama pull nomic-embed-text
```
*(Find your server's internal IP using `ip addr` or `ifconfig`. For example: `10.0.0.50`)*

**2. On the Spokes (Client Laptops):**
Your team members only need to install the lightweight Sahayak CLI. They simply point it to the Hub's IP and port:
```sh
export OLLAMA_HOST="http://10.0.0.50:11434"
sahayak ask "how is my local systemd docker service doing?"
```

**Option C: Air-Gapped Appliance (No Ollama)**
For highly restricted environments (like production jump boxes or secure data centers) where you cannot install background daemons, Sahayak can run fully standalone. You just drop the Sahayak binary, `llama-server`, and a model file into a single folder and run it. See the **[Embedded Appliance Guide](./docs/embedded-appliance.md)** for setup.

### 3. Ask a Question
Check that everything is wired up, then start troubleshooting!
```sh
sahayak doctor
sahayak ask "how is my local systemd docker service doing?"
```

---

## 🧩 Adding Tools (Cartridges)

Out of the box, Sahayak is a blank slate. You teach it how to manage your infrastructure by installing **Cartridges**. 

First, add the official registry to your CLI:
```sh
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-cli/main/registry/index.json
```

### 📚 The Official Catalog
You can search the registry using `sahayak cartridge search`, or install the following official cartridges immediately:

| Cartridge | Description | Install Command |
|-----------|-------------|-----------------|
| **`k8s`** | Kubernetes cluster diagnosis, pod logs, and pod restarts. | `sahayak cartridge install k8s` |
| **`systemd`** | Linux service management, journalctl logs, and daemon troubleshooting. | `sahayak cartridge install systemd` |
| **`docker`** | Docker containers: ps, logs, restart, stop, prune. | `sahayak cartridge install docker` |
| **`redis`** | Redis data stores: ping, memory info, flushall. | `sahayak cartridge install redis` |
| **`linux-net`** | Linux networking: ping, lsof (ports), dig. | `sahayak cartridge install linux-net` |
| **`aws-ec2`** | AWS EC2: list, start, stop instances. | `sahayak cartridge install aws-ec2` |
| **`postgres`** | PostgreSQL: active queries, db sizing. | `sahayak cartridge install postgres` |
| **`nginx`** | Nginx: test config (-t), reload without dropping packets. | `sahayak cartridge install nginx` |
| **`git`** | Git: repository status and recent logs. | `sahayak cartridge install git` |

*(More cartridges like AWS are actively being developed!)*

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
- **[Self-Learning Engine](./docs/self-learning.md)**: How Sahayak safely observes your terminal to suggest new templates.

### Building from Source
Sahayak is a pure, CGO-free Go binary.
```sh
make build   # Compiles to ./bin/sahayak (cross-compiles trivially)
make test    # 16 tested packages
```

## License

[MIT](./LICENSE) © 2026 ZentienceLabs.

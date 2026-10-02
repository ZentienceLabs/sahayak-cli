---
layout: default
title: Home
nav_order: 1
---

**Navigation:** [Home](./README.md) | [Installation](./installation.md) | [Configuration](./configuration.md) | [Commands](./commands.md) | [Cartridges](./cartridges.md) | [Ops Teams](./ops-teams.md) | [Self-Learning](./self-learning.md) | [Embedded Appliance](./embedded-appliance.md)

***

# Sahayak Documentation

Sovereign AI DevOps CLI — plain language → inspected, approved commands. CPU-only,
air-gappable, MIT.

## Contents

| Doc | What's in it |
|---|---|
| [Installation](./installation.md) | Install the CLI + a model backend (Ollama / embedded) |
| [Configuration](./configuration.md) | Every env var, flag, and where state lives |
| [Commands](./commands.md) | Full reference for every command and option |
| [Cartridges](./cartridges.md) | Install / build / publish / sign tool cartridges + the registry |
| [Self-learning](./self-learning.md) | How Sahayak learns from outcomes and proposes improvements |
| [Embedded appliance](./embedded-appliance.md) | Sealed, air-gapped build: bundling `llama-server` + a GGUF model |

## 60-second start

```sh
go install github.com/ZentienceLabs/sahayak-cli/cmd/sahayak@latest
ollama serve & ollama pull qwen3:4b-instruct
sahayak doctor
sahayak ask "how is web-api doing"
```

See also the design docs at the repo root: [`ARCHITECTURE.md`](../ARCHITECTURE.md),
[`CARTRIDGE-ARCHITECTURE.md`](../CARTRIDGE-ARCHITECTURE.md), [`RUNBOOK.md`](../RUNBOOK.md),
[`COVERAGE.md`](../COVERAGE.md).

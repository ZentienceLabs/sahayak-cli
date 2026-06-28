# Sahayak (सहायक — "helper")

> A **sovereign** AI command-line assistant for DevOps & sysadmins that runs entirely on
> infrastructure you control. Describe what you need in plain language; Sahayak routes it to
> a verified procedure, **explains the command, shows its risk, and waits for your approval**.
> Single static Go binary, **CPU-only, air-gappable** — your data, secrets, and shell history
> never leave your box.

MIT licensed. Cartridges (tool packs) are distributed via a separate, signed registry:
[ZentienceLabs/sahayak-registry](https://github.com/ZentienceLabs/sahayak-registry).

## The idea: an LLM that understands + swappable knowledge cartridges + deterministic guards

A small local model **can't** reliably author ops commands — so Sahayak doesn't let it. The
model only **understands** (route the request, fill typed slots); **Go acts** (builds the
command from a human-authored template, classifies risk, validates, runs); the **human
authors and approves**. You teach Sahayak a tool by installing a **cartridge** (data), not by
writing code.

- **Engine** (tool-agnostic Go): slot extraction, command templates, cross-cartridge peer
  routing, multi-step composition, risk gating, dry-run, redaction, self-learning.
- **Cartridges** (data, per tool): commands (`intent → command + typed slots + risk`) **and**
  curated how-to knowledge (RAG), shipped together. `k8s` and `systemd` are built in; add more.
- **Brain** (local LLM via Ollama or an embedded `llama-server`): understands + narrates.

See [`CARTRIDGE-ARCHITECTURE.md`](./CARTRIDGE-ARCHITECTURE.md) and [`ARCHITECTURE.md`](./ARCHITECTURE.md).

## Install

```sh
go install github.com/ZentienceLabs/sahayak-cli/cmd/sahayak@latest
# or build from source:
git clone https://github.com/ZentienceLabs/sahayak-cli && cd sahayak-cli && make build  # -> ./bin/sahayak
```

You also need a local model backend — [Ollama](https://ollama.com) for dev:

```sh
ollama serve &
ollama pull qwen3:4b-instruct       # default brain (Apache-2.0)
ollama pull nomic-embed-text        # recommended: true-semantic routing
```

## Quick start

```sh
sahayak doctor                                  # check backend + config
sahayak                                          # interactive shell (pick a model, then type)
sahayak ask "how is web-api doing"               # composed health rollup, deterministic
sahayak ask "list the configmaps for web-api"    # one read-only command, filtered in Go
export SAHAYAK_EMBEDDER=ollama:nomic-embed-text  # best phrasing coverage
```

Mutating commands (`restart`, `scale`, …) always stop at the approval gate; read-only ones
auto-run. Use `! <command>` in the shell to run something yourself (still risk-gated).

## Tools are cartridges — install more, or build your own

```sh
# add the official registry, browse, install (downloads + verifies checksum & signature)
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-registry/main/index.json
sahayak cartridge trust add fulEJgyF8DPnFR7cKcB00whsinJc2KXHMy692WfY/3M=
sahayak cartridge search ""
sahayak cartridge install k8s

# build your own from a how-to doc + a templates file, then publish it
sahayak cartridge build --name redis --kb redis-howto.md --templates redis.json
sahayak cartridge keygen        # sign it; users `cartridge trust add` your public key
```

A cartridge declares an **applicability probe**, so a tool that isn't present on the host is
pruned and never mis-routes (peer cartridges, no primary/secondary).

## It learns (safely)

Sahayak records **deterministic** signals (command exit codes, routing hits) and proposes
improvements — it never changes behavior on its own, and never lets the model judge itself.

```sh
sahayak learn suggest    # "you ran X 3× successfully — templatize it?", failing intents, gaps
sahayak learn promote --intent restart-web --phrase "bounce the web app"   # human-gated → overlay
```

## How a turn works

1. Your request + machine context → routed (by meaning) to a cartridge intent, or to the
   adaptive investigate loop if nothing matches.
2. The slot engine grounds typed slots; **Go assembles the command** from the human-authored
   template (the model never emits a command string).
3. Go classifies risk: read-only may auto-run; mutating/destructive **stop at the gate**
   (`[a]pprove / [e]dit / [r]eject / [s]kip`); mutations are `--dry-run`-validated first.
4. On failure, the diagnosis engine feeds the redacted exit-code/stderr back for a root cause.

## Safety model

- Commands are structured `command`+`args[]` — **no shell-injection surface**.
- **The model never decides risk or authors a command** — deterministic Go does.
- Secrets are redacted before reaching the model or any log; Sahayak never reads secret stores.
- Installed cartridges are **checksum- + signature-verified**, and their commands are shown
  for review before install. Set `SAHAYAK_REQUIRE_SIGNED=1` to require signatures.

## Docs

- [`docs/`](./docs/) — **start here**: [install](./docs/installation.md) · [configure](./docs/configuration.md) · [commands & options](./docs/commands.md) · [cartridges](./docs/cartridges.md) · [self-learning](./docs/self-learning.md).
- [`RUNBOOK.md`](./RUNBOOK.md) — operational usage, every command, config knobs.
- [`CARTRIDGE-ARCHITECTURE.md`](./CARTRIDGE-ARCHITECTURE.md) — the cartridge model, registry, self-learning, tech stack.
- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — the division of labor (model / Go / human) and why.
- [`COVERAGE.md`](./COVERAGE.md) — op coverage checklist.
- [`project.md`](./project.md) — full vision and roadmap.

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

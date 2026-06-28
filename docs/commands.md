# Command Reference

Every Sahayak command and option. Run `sahayak help` for the short version.

```
sahayak                                start the interactive shell (no args, on a TTY)
sahayak <command> [subcommand] [args] [flags]
```

Top-level commands: `ask` · `shell` · `models` · `cartridge` · `learn` · `knowledge` ·
`memory` · `doctor` · `version` · `help`.

---

## `ask` — one-shot request
Turn a plain-language request into an inspected, approved, executed action.

```sh
sahayak ask "<what you want>" [flags]
```

Flags: `--engine`, `--endpoint`, `--model`, `--approve-all-readonly` (default true),
`--no-tui`, `--investigate`, `--plan`, `--max-steps <n>` (default 8). See
[Configuration](./configuration.md#flags-apply-to-ask-and-doctor).

```sh
sahayak ask "how is web-api doing"                       # composed health rollup
sahayak ask "list the configmaps for web-api"            # one read-only command
sahayak ask "why is web-api failing"                     # log error scan
sahayak ask --plan "reload nginx after editing the server block"
sahayak ask --max-steps 5 "find failing pods across the cluster"
sahayak ask --no-tui "show node pressure conditions"     # plain gate (good for CI/SSH)
sahayak ask --approve-all-readonly=false "get deployments for web-api"  # confirm reads too
```

Read-only steps auto-run (unless `--approve-all-readonly=false`); mutating/destructive steps
stop at the gate `[a]pprove / [e]dit / [r]eject / [s]kip`. Mutations are `--dry-run`-validated
before they run.

---

## `shell` — interactive REPL
Pick a model once, then type requests in a loop (one warm agent + background learning).

```sh
sahayak                       # on a TTY, this launches the shell
sahayak shell                 # explicit; also: repl, interactive
sahayak shell --model qwen2.5-coder:7b
```

Flags: `--endpoint`, `--model`, `--engine`.

Inside the shell:
```
sahayak> list configmaps for web-api
sahayak> how is web-api doing
sahayak> ! kubectl get ns       # run a command yourself (still risk-gated)
sahayak> models                 # re-pick the model mid-session
sahayak> help                   # built-in shell commands
sahayak> exit                   # or quit / :q / Ctrl-D
```

---

## `models` — list installed models
```sh
sahayak models                # lists Ollama models; the default is marked
```

---

## `cartridge` (alias `cart`) — manage tool packs
Full details in [Cartridges](./cartridges.md).

```sh
sahayak cartridge list                         # built-in + installed
sahayak cartridge search [query]               # search configured registries
sahayak cartridge install <name|file.json|url> # name → registry (checksum+sig verified)
sahayak cartridge remove <name>                # alias rm / uninstall
sahayak cartridge where                        # the install dir
sahayak cartridge build  --name N [--kb doc.md] [--templates t.json] [--command "probe"] [--from-pack P] [-o out.json]
sahayak cartridge registry add <url|path>
sahayak cartridge registry list
sahayak cartridge trust add <public-key>
sahayak cartridge trust list
sahayak cartridge keygen                        # make a signing keypair
sahayak cartridge sign <file.json> --key <privkey-file>
```

---

## `learn` — self-learning
Full details in [Self-learning](./self-learning.md).

```sh
sahayak learn suggest                           # drafts: promote / fix / cover-gap (alias list, show)
sahayak learn promote --intent <name> --phrase "a,b" [--cartridge learned] [--command "..."]
sahayak learn forget                            # clear the learning log (alias clear, reset)
```

---

## `knowledge` (alias `kb`) — offline RAG packs
```sh
sahayak knowledge install <file.sahayakpack>
sahayak knowledge list
sahayak knowledge search [--pack N] [-k 5] "<query>"
sahayak knowledge build --name N --from FILE -o OUT.sahayakpack [--command kubectl]
sahayak knowledge remove <name>                 # alias rm
```
`--from` accepts a `.jsonl` of chunks or a `.txt`/`.md` (chunked on blank lines). Embedding
cost is paid once, at build time, with your configured `SAHAYAK_EMBEDDER`.

---

## `memory` (alias `mem`) — long-term notes you curate
These DO influence future runs (injected as grounding).

```sh
sahayak memory add "prod cluster is AKS, 6 nodes, namespace prefix acme-"
sahayak memory list
sahayak memory search "cluster"
sahayak memory forget "<substring>"
```

---

## `doctor` — health check
```sh
sahayak doctor                 # engine, endpoint, model, embedder, packs, memory, backend
```
Flags: `--engine`, `--endpoint`, `--model`.

---

## `version` / `help`
```sh
sahayak version                # build info (also --version, -v)
sahayak help                   # usage (also --help, -h)
```

---

## The routing pipeline (what happens to a request)
1. **Cartridge engine** (default) — match by meaning across all installed tools → run the
   grounded template (or composed rollup). Absent tools are pruned by their applicability probe.
2. **Investigate loop** — if nothing matches, the model proposes one read-only step at a time;
   Go condenses/keyword-filters output and feeds it back, until it concludes or hits `--max-steps`.
3. **Honest fallback** — if nothing grounds an answer, Sahayak says so and points at a phrasing
   or the `!` escape hatch; it never guesses a command.

(Set `SAHAYAK_LEGACY=1` to use the older regex-playbook + semantic-router + classifier pipeline.)

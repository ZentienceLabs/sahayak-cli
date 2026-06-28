# Contributing to Sahayak

Thanks for helping! There are two ways to contribute: **the CLI** (this repo) and
**cartridges** (tool packs, usually via the [registry](https://github.com/ZentienceLabs/sahayak-registry)).
Often you don't need to touch Go at all — a new tool is *data*.

## Code of conduct
Be kind and constructive. Assume good faith.

## Contributing a cartridge (no Go required)

A cartridge teaches Sahayak a tool as data (commands + knowledge). See
[docs/cartridges.md](./docs/cartridges.md) for the full format.

1. Build it:
   ```sh
   sahayak cartridge build --name redis --kb redis-howto.md --templates redis.json --command "redis-cli ping"
   ```
2. Test it locally:
   ```sh
   sahayak cartridge install ./redis.json
   sahayak ask "restart the cache"        # confirm it routes + grounds correctly
   ```
3. Publish via the registry — open a PR to
   [`sahayak-registry`](https://github.com/ZentienceLabs/sahayak-registry):
   - add `cartridges/redis.json`,
   - add an `index.json` entry with `url`, `sha256` (`sha256sum cartridges/redis.json`), and
     a `signature` (`sahayak cartridge sign cartridges/redis.json --key <your-priv-key>`),
   - include your **public** key so users can `cartridge trust add` it.

Cartridges run commands — keep templates **least-privilege**, set the correct `risk` tier,
and prefer read-only intents. Reviewers will check the declared commands.

## Contributing to the CLI

### Setup
```sh
git clone https://github.com/ZentienceLabs/sahayak-cli && cd sahayak-cli
make build && make test
```
Requires Go 1.25+. Everything builds **CGO-free** (`CGO_ENABLED=0`) and cross-compiles to
linux/darwin/windows × amd64/arm64 — keep it that way (no cgo dependencies).

### Before you push
```sh
make fmt     # gofmt -w
make vet     # go vet ./...
make test    # all packages must pass
```

### Architecture & where things go
Read [ARCHITECTURE.md](./ARCHITECTURE.md) and [CARTRIDGE-ARCHITECTURE.md](./CARTRIDGE-ARCHITECTURE.md) first.
The guiding rule:

> **The model understands; Go acts; the human authors and approves.**

- New tool support → a **cartridge** (data), not Go.
- New generic capability (a slot extractor, output processor, command shape) → the **engine**
  (`core/slots`, `core/cartridge`, `core/agent`), with tests.
- Never let the model author a command string or decide risk — those stay deterministic in Go.
- Anything that mutates state must be risk-classified and pass the approval gate.

### Style
- Match the surrounding code; explicit return types on exported funcs; named exports.
- Keep new dependencies out unless essential (the project leans on the stdlib + Charm TUI).
- Add tests for new behavior; tests must be hermetic (the offline `hash` embedder makes the
  router/index testable without a backend).

### Commits & PRs
- Conventional commits: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`.
- Keep commits atomic; describe the *why* in the body.
- Open a PR against `main`; ensure `make fmt vet test` is clean and CI passes.

## Reporting bugs / ideas
Open a GitHub issue with repro steps (the command you ran, what you expected, what happened).
For a routing miss, include the exact phrasing — often the fix is just a catalog line.

## License
By contributing you agree your contributions are licensed under the project's
[MIT License](./LICENSE).

# Cartridges

A **cartridge** teaches Sahayak a tool — as *data*, not code. It bundles **command
templates** (`intent → command + typed slots + risk`) and **curated knowledge** (how-to /
scenarios for RAG). `k8s` and `systemd` ship built in; everything else is installable.

Cartridges are **peers** — no primary/secondary. A request is routed by meaning across all
of them; a tool that isn't present on the host (per its applicability probe) is pruned so it
can't mis-route.

## Use installed cartridges

```sh
sahayak cartridge list                 # built-in + installed, with template/intent counts
sahayak cartridge where                # ~/.sahayak/cartridges
```
Once installed, just ask naturally — `sahayak ask "restart the web-api deployment"` routes to
the right cartridge intent, grounds the slots, builds the command, and gates it.

## Install from the registry (verified)

```sh
sahayak cartridge registry add https://raw.githubusercontent.com/ZentienceLabs/sahayak-registry/main/index.json
sahayak cartridge trust add fulEJgyF8DPnFR7cKcB00whsinJc2KXHMy692WfY/3M=   # ZentienceLabs publisher key
sahayak cartridge search ""            # list everything; or: search redis
sahayak cartridge install k8s          # downloads → verifies checksum + signature → installs
```

On install Sahayak prints the commands the cartridge can run and their risk tiers — review
them before trusting it. Add `SAHAYAK_REQUIRE_SIGNED=1` to refuse unsigned cartridges.

You can also install directly from a file or URL (no registry):
```sh
sahayak cartridge install ./redis.json
sahayak cartridge install https://example.com/redis.json
sahayak cartridge remove redis         # uninstall (built-ins can't be removed)
```

Multiple registries are peers — add an official one and your private/internal one.

## Build your own

```sh
# from a how-to doc + a templates file
sahayak cartridge build --name redis \
  --kb redis-howto.md \
  --templates redis-templates.json \
  --command "redis-cli ping" \         # applicability probe (relevant only if this exits 0)
  -o redis.json

# fold an existing knowledge pack's chunks in as KB
sahayak cartridge build --name acme --from-pack acme-ops --command "kubectl config current-context"
```

A **templates file** is a JSON array. Each template:
```json
[
  {
    "intent": "restart",
    "command": "systemctl",
    "args": ["restart", "{unit}"],
    "risk": "mutating",
    "processor": "raw",
    "shape": "simple",
    "slots": [
      { "name": "unit", "extractor": "after-verb", "required": true, "verbs": ["restart","bounce"] }
    ]
  }
]
```

**Slot extractors** (how a placeholder is grounded from the request):
| Extractor | Grounds | Example |
|---|---|---|
| `hyphenated-token` | first identifier-like token with a `-` | `web-api` |
| `after-preposition` | entity after for/in/of/on… | "logs **for** web-api" |
| `after-verb` | entity after an action verb (give `verbs`) | "restart **nginx**" |
| `content-keyword` | the longest distinctive word | "flag for **telemetry**" |
| `enum` | a value from `values` (returns canonical) | `cm` → `configmaps` |

**Risk tiers:** `read-only` (auto-runs), `mutating` (gated), `destructive` (gated, loudly).
**Shapes:** `simple` (one command), `resolve-fanout` (resolve a set, run a per-item command),
`rollup` (compose several into one verdict). **Processors** post-process output:
`raw`, `error-extract`, `filter-summarize`, `configmap-search`.

## Sign & publish

Cartridges run commands, so publishing should be signed (ed25519).

```sh
sahayak cartridge keygen                                  # prints a public + private key
#   save the PRIVATE key somewhere secret (NOT in the repo); publish the PUBLIC key
sahayak cartridge sign redis.json --key ./private.key     # prints a base64 signature
```

Then in your registry `index.json`:
```json
{
  "cartridges": [
    {
      "name": "redis",
      "version": "1.0.0",
      "tool": "redis-cli",
      "description": "Redis ops",
      "url": "https://raw.githubusercontent.com/<org>/<registry>/main/cartridges/redis.json",
      "sha256": "<sha256sum of redis.json>",
      "signature": "<output of cartridge sign>"
    }
  ]
}
```

Users `cartridge trust add <your-public-key>` once, then every install from you is
authenticity-verified. See the registry repo:
[ZentienceLabs/sahayak-registry](https://github.com/ZentienceLabs/sahayak-registry).

## The trust model
- **Checksum** (`sha256`) → integrity: the bytes weren't corrupted/tampered in transit.
- **Signature** (ed25519 vs trusted keys) → authenticity: a publisher you trust authored them.
- **Install-time review** → you see the commands/risk before it's active.
- **Applicability probe** → a tool absent on the host never routes.

# Sahayak — Op Coverage Checklist

> The honest finish line (see `ARCHITECTURE.md` → "What 'done' means"): **cover the
> ops the team actually runs, phrased naturally** — not "handle anything." The real op
> set is *finite*. This file is that whiteboard. Walk it to see exactly how close to
> "done" v1 is.
>
> Legend: ✅ deterministic playbook · 🟡 partial (model investigate loop / knowledge
> pack, not deterministic) · ⬜ gap (no coverage). Grounded in the real debug runbook
> `C:\tmp\acme-k8s-az-commands.md` (§A–E) + standard k8s ops.

## Score
- **Deterministic playbooks today (7):** list, logs, image, rollout, restart, verifyenv, searchcfg
- **Composite ops (1):** status rollup (image + rollout + recent-errors → HEALTHY/DEGRADED verdict)
- **Covered ✅: 10 / ~40 · Partial 🟡: 8 · Gap ⬜: ~22**
- Read this as: the *common* asks are solid; the *long tail* is mostly the investigate loop (unreliable) or absent.

## Composite ops (multi-playbook synthesis — the composition layer)
| Op | Natural phrasing | Status |
|---|---|---|
| Status rollup (image+rollout+errors → verdict) | "how is acme-web doing", "is acme-web healthy" | ✅ status composite |
| Drift / compare across namespaces | "is demo on a different build than dev" | ⬜ next composite candidate |
| Pre-restart safety rollup | "is it safe to restart acme-web" | ⬜ gap |

---

## A. Read / inspect (read-only → auto-run)
| # | Op | Natural phrasing | Status |
|---|---|---|---|
| 1 | List resources for an app | "list configmaps for acme-web" | ✅ list |
| 2 | Logs / error hunt | "why is acme-web failing" | ✅ logs |
| 3 | Image / tag | "what image is acme-web running" | ✅ image |
| 4 | Rollout status | "is acme-web rolled out" | ✅ rollout |
| 5 | Verify env var live (`exec -- printenv`) | "is DEBUG_MODE set in acme-web" | ✅ verifyenv |
| 6 | Search configmap CONTENTS | "is there a config flag for workflow" | ✅ searchcfg |
| 7 | **Which cluster am I on** (§A1 — pin target) | "what context am I on / am I on dev" | ⬜ **gap (high value, trivial)** |
| 8 | Describe a resource (events/conditions) | "describe acme-web" | ⬜ gap |
| 9 | Pod health table (restarts/crashloops) | "are the acme-web pods healthy" | 🟡 investigate loop |
| 10 | Previous-container logs (crashloop) | "why did acme-web crash last time" | 🟡 investigate (`--previous`) |
| 11 | Rollout history / revisions | "show acme-web rollout history" | ⬜ gap |
| 12 | Cluster events | "any recent events in acme-dev" | ⬜ gap |
| 13 | Resource usage (top pods/nodes) | "what's using cpu in acme-dev" | ⬜ gap |
| 14 | Node status / conditions | "are the nodes healthy" | 🟡 investigate |
| 15 | Ingress / route lookup | "what ingress points to acme-web" | 🟡 list ingresses |
| 16 | Service endpoints | "what endpoints back acme-web" | ⬜ gap |
| 17 | PVC / storage | "is acme-web's volume bound" | ⬜ gap |
| 18 | HPA status | "is acme-web autoscaling" | ⬜ gap |

## B. Mutating (approval-gated + dry-run)
| # | Op | Natural phrasing | Status |
|---|---|---|---|
| 19 | Restart / rollout restart (§B1) | "restart acme-web" | ✅ restart |
| 20 | Scale a deployment | "scale acme-web to 3" | ⬜ **gap (high value)** |
| 21 | Rollback (`rollout undo`) | "roll back acme-web" | ⬜ **gap (high value)** |
| 22 | Set image / bump tag | "set acme-web to v1.0.17" | ⬜ gap |
| 23 | Set/patch env on a deployment | "set DEBUG_MODE=true on acme-web" | ⬜ gap (read side ✅) |
| 24 | Delete/recreate a pod | "delete the stuck acme-web pod" | ⬜ gap |
| 25 | Cordon / drain a node | "drain node X" | ⬜ gap (destructive) |
| 26 | Apply / patch a manifest | "apply this manifest" | ⬜ gap |

## C. Diagnostics (genuinely novel → model loop is the right home)
| # | Op | Natural phrasing | Status |
|---|---|---|---|
| 27 | Root-cause a crashloop | "why does acme-web keep restarting" | 🟡 investigate |
| 28 | Deployment skew / version mismatch (§A) | "is demo on a different build than dev" | ⬜ gap |
| 29 | OOMKilled detection | "did anything get OOM killed" | ⬜ gap |
| 30 | Pending / unschedulable pod | "why won't acme-web schedule" | ⬜ gap |
| 31 | ImagePullBackOff | "why can't it pull the image" | 🟡 investigate |

## D. Azure (`az`) — currently knowledge-pack only, NO playbooks
| # | Op | Natural phrasing | Status |
|---|---|---|---|
| 32 | Get AKS credentials (§E gotcha) | "get creds for the dev cluster" | 🟡 knowledge pack |
| 33 | List clusters / subscriptions (§D1) | "what clusters are there" | ⬜ gap |
| 34 | GPU quota via `az rest` Microsoft.Quota (§D2) | "did the T4 quota request go through" | 🟡 knowledge pack |
| 35 | ACR image list / tags | "what tags are in the registry" | ⬜ gap |

## E. Safety / meta (must stay true regardless of coverage)
| # | Invariant | Status |
|---|---|---|
| 36 | Secret boundary — never read secret stores; user runs via `!` (§C) | ✅ enforced |
| 37 | Mutations gated + dry-run-validated first | ✅ |
| 38 | Honest fallback when nothing grounds | ✅ |
| 39 | Typed slots → Go builds command (model never emits a string) | ✅ |
| 40 | Semantic router so phrasing ≠ a code change (`nomic-embed-text`) | ✅ |

---

## Recommended next 6 (highest value / lowest effort — finishes the common path)
Each is a `core/playbook` matcher + catalog phrasings + (if mutating) the existing gate.
None needs the model. In rough priority:

1. **#7 context / "which cluster am I on"** — trivial, read-only, §A1 is *step one* of every real debug session. Biggest value-per-line.
2. **#21 rollback (`rollout undo`)** — the natural partner to restart; mutating, reuses the gate + dry-run.
3. **#20 scale** — one slot (replica count), mutating, very common.
4. **#9/#27 pod-health as a deterministic playbook** — promote the investigate-loop health table into a real playbook so "are the pods healthy / why restarting" stops depending on the model.
5. **#11 rollout history** + **#8 describe** — read-only, surfaces events/revisions (the data the loop currently scrapes unreliably).
6. **#13 top (resource usage)** — read-only, one command, frequently asked.

Doing these takes deterministic coverage from **9 → ~15** and closes most of the *common* path. The remaining gaps are long-tail (D/E Azure, destructive node ops) — fine to leave to `!` + knowledge packs until asked.

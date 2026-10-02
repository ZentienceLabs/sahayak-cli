# Sahayak router fine-tune (OPTIONAL)

Fine-tunes a small Apache-2.0 model to be Sahayak's **intent router** — the one call
that does **route** (which playbook) + **extract** (fill the slots), emitting exactly
the JSON the CLI already consumes:

```json
{"intent": "logs", "app": "acme-web", "resource": "", "env_var": ""}
```

It does **not** touch planning, command construction, validation, or narration — those
stay in deterministic Go. (Narration is also mostly deterministic Go already, which is
why it's not a fine-tune target here — see `../ARCHITECTURE.md`.)

## Read this first — is it even worth it?

**Probably build the semantic router and measure before you fine-tune.** The CLI now
ships a data-driven **semantic router** (`core/router`) that already widens phrasing
coverage with *no training, no GPU, fully inspectable* — you add a phrasing by editing
`core/router/catalog.txt`. Fine-tuning is an *optimization on top of that*, not a
prerequisite, and it carries a real risk you have already hit once:

> Your prior router fine-tune (the research project) **scored BELOW the base model**
> (15/22 vs 19/22) — catastrophic forgetting from too-aggressive training.

So this pipeline bakes in the lessons: an Apache-2.0 base (shippable), a **gentle**
recipe, and a **mandatory eval-against-base gate**. Ship the fine-tune **only if it
clearly beats the base** on the held-out split. If it doesn't, you've lost nothing — the
deterministic playbooks + semantic router are already doing the job.

## The flow

```
gen_dataset.py   ──►  router_dataset_train.jsonl  (+ _eval, held out)     [offline, no GPU]
finetune_modal.py ──►  QLoRA → merge → q8_0 GGUF on a Modal A10G          [your Modal creds]
Modelfile        ──►  ollama create sahayak-router                        [local]
eval.py          ──►  score BASE vs FINE-TUNE on the held-out split       [the gate]
```

## Steps

```bash
# 0) one-time: Modal account (a security boundary — uses YOUR credentials)
pip install modal && modal token new

# 1) build the dataset (deterministic, offline)
python gen_dataset.py                      # -> router_dataset_train.jsonl + _eval.jsonl

# 2) train on a Modal GPU (verify MODEL_NAME's tag+license first — see the script)
modal run finetune_modal.py
modal volume get sahayak-router-out sahayak-router-q8_0.gguf .

# 3) import into Ollama (the CLI's backend)
ollama create sahayak-router -f Modelfile

# 4) THE GATE — score both, ship only if the fine-tune wins
python eval.py --model qwen3:4b-instruct --data router_dataset_eval.jsonl   # base
python eval.py --model sahayak-router    --data router_dataset_eval.jsonl   # fine-tune

# 5) if (and only if) it wins, use it:
sahayak ask --model sahayak-router "is acme-web rolled out"
#   or set SAHAYAK_MODEL=sahayak-router
```

## Files

| File | Role |
|------|------|
| `gen_dataset.py` | Offline, deterministic dataset generator. Output JSON matches `core/agent/classify.go` (system prompt + `{intent,app,resource,env_var}`) verbatim, so the model is drop-in. |
| `finetune_modal.py` | One-command Modal QLoRA → merge → q8_0 GGUF. Apache-2.0 base, gentle recipe. |
| `Modelfile` | Imports the GGUF into Ollama as `sahayak-router` (temp 0). |
| `eval.py` | The eval-against-base gate: scores intent + slot accuracy on the held-out split via Ollama. |

## Licensing

The embedded appliance may only ship **Apache-2.0 / MIT** weights (see memory
`sahayak-model-licensing-rule`). The base here is Qwen3-4B-Instruct (Apache-2.0) — **verify
the exact unsloth tag and its license before training.** Gemma is excluded from the
embedded target; use it only for a dev-only model run via Ollama, never shipped.

## How this fits the architecture

The fine-tuned model slots into the **same place** the base model's classifier already
sits — step 3 of the routing pipeline, *after* the regex playbooks and the semantic
router, gated by `MightBeK8s`, and validated by `playbook.FromClassification` (grounding
+ shape checks). Even a perfectly-trained router is still **proposing, never disposing**:
Go validates every slot and constructs every command. So a bad fine-tune degrades to
"no plan / fall through", never to a wrong command. That's why this is safe to try — and
safe to skip.

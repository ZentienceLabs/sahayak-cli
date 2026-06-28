#!/usr/bin/env python3
"""
Generate the router fine-tune dataset — OFFLINE, no API, pure stdlib.

WHAT THIS PRODUCES
------------------
JSONL in OpenAI chat format whose assistant turn is EXACTLY the JSON the Sahayak CLI
already consumes from its intent classifier:

    {"intent": "...", "app": "...", "resource": "...", "env_var": "..."}

…paired with the SAME system prompt the CLI sends (kept verbatim below, so the
fine-tuned model is a drop-in replacement for the base on the classify call). The
model learns the two jobs a small model is reliable at — ROUTE (pick the intent) and
EXTRACT (pull the grounded slots) — and nothing else. Command construction, validation,
and narration stay in deterministic Go.

WHY TEMPLATED (not model-generated)
-----------------------------------
The whole point of the project is sovereignty and reproducibility. This generator is
deterministic (fixed seed) and needs no network or strong model: it expands a large set
of phrasing templates over a diverse pool of apps / resources / env-vars / namespaces,
plus hard negatives that must route to "none". You can regenerate it identically on any
machine. (If you later want more phrasing diversity, you can append model-paraphrased
lines — but the base set already covers the catalog + the real failures we logged.)

USAGE
    python gen_dataset.py --out-dir .            # writes train + eval JSONL
    python gen_dataset.py --n 4000 --eval-frac 0.1

OUTPUT
    router_dataset_train.jsonl
    router_dataset_eval.jsonl   (held out — used to score fine-tune vs BASE)
"""

import argparse
import json
import os
import random

# The system prompt MUST match core/agent/classify.go:classifySystemPrompt verbatim, so
# training matches inference. If you change it in the CLI, change it here and retrain.
SYSTEM_PROMPT = """You are Sahayak's intent router. Map the operator's request to ONE intent and extract the target. Do NOT plan or propose commands.

Intents:
- "list": list a kind of resource for an app/namespace (e.g. "configmaps for acme-web"). Set "resource" (configmaps|services|deployments|pods|secrets|ingress|statefulsets|jobs) and "app" (the app or namespace keyword).
- "logs": find why an app is failing / read its error logs. Set "app".
- "image": what container image an app runs. Set "app".
- "rollout": an app's rollout/deployment status. Set "app".
- "restart": restart/redeploy an app. Set "app".
- "verifyenv": check whether an environment variable is set in an app. Set "app" and "env_var" (the VARIABLE_NAME).
- "none": anything else, ambiguous, pod-level crash analysis, or a request that needs multi-step investigation.

Rules:
- "app" MUST be copied verbatim from the request (e.g. "acme-web"). Never invent a name. If you can't find one, use "none".
- Prefer "none" when unsure. Respond with a single JSON object only."""

# Diverse pools so the model generalizes beyond the acme-* family it was failing on.
APPS = [
    "acme-web", "acme-worker", "acme-ai", "acme-api", "acme-ui",
    "payments-service", "auth-gateway", "billing-worker", "search-indexer",
    "notification-service", "order-api", "user-profile", "image-resizer",
    "checkout-web", "inventory-sync", "report-generator", "email-dispatcher",
]
NAMESPACES = ["acme-dev", "acme-demo", "prod", "staging", "kube-system", "default"]
RESOURCES = ["configmaps", "services", "deployments", "pods", "secrets", "ingress", "statefulsets", "jobs"]
RESOURCE_ALIASES = {
    "configmaps": ["configmaps", "configmap", "cm"],
    "services": ["services", "service", "svc"],
    "deployments": ["deployments", "deployment", "deploy"],
    "pods": ["pods", "pod"],
    "secrets": ["secrets", "secret"],
    "ingress": ["ingress", "ingresses"],
    "statefulsets": ["statefulsets", "statefulset", "sts"],
    "jobs": ["jobs", "job"],
}
ENV_VARS = [
    "CONSOLE_WORKFLOW_REDESIGN", "FEATURE_X_ENABLED", "DEBUG_MODE", "REDIS_URL",
    "DATABASE_URL", "LOG_LEVEL", "OAUTH_ENABLED", "RATE_LIMIT", "CACHE_TTL",
    "WORKFLOW_AI_BRIDGE_BASE_URL", "ENABLE_TELEMETRY", "MAX_CONNECTIONS",
]

# Phrasing templates per intent. {app} {res} {ns} {env} are filled per-sample. Each
# template is one way an operator might phrase the intent; more templates = more
# phrasing coverage learned. These mirror core/router/catalog.txt + the real misroutes.
TEMPLATES = {
    "list": [
        "list {res} for {app}", "show me the {res} in {app}", "get {res} for {app}",
        "which {res} of {app} are there", "provide the {res} list for {app}",
        "display all {res} in {ns}", "what {res} exist for {app}",
        "enumerate the {res} in {ns}", "can you list {res} for {app}",
        "show {res} belonging to {app}", "{res} for {app} please",
    ],
    "logs": [
        "why is {app} failing", "show me errors in {app} logs", "{app} is crashing",
        "what is wrong with {app}", "debug the {app} logs", "{app} keeps throwing exceptions",
        "look at why {app} is broken", "investigate the failures in {app}",
        "{app} is returning 500s", "check the error logs for {app}",
        "what's going wrong with {app}",
    ],
    "image": [
        "what image is {app} running", "show the image tag for {app}",
        "which version is {app} on", "what container image does {app} use",
        "tell me {app} current tag", "what's the image for {app}",
    ],
    "rollout": [
        "rollout status of {app}", "is {app} rolled out",
        "did the {app} deployment finish rolling out", "has {app} finished its rollout",
        "check the rollout for {app}", "is {app} deployment healthy and rolled out",
    ],
    "restart": [
        "restart {app}", "redeploy {app}", "bounce {app}",
        "please restart the {app} deployment", "cycle the pods for {app}",
        "do a rolling restart of {app}",
    ],
    "verifyenv": [
        "is {env} set in {app}", "verify {env} in {app}", "check the value of {env} in {app}",
        "what is {env} set to in {app}", "confirm {env} is enabled in {app}",
        "is the {env} variable configured in {app}",
    ],
}

# Hard negatives → "none": chitchat, pod-level, multi-step, ambiguous, config mutation.
NONE_TEMPLATES = [
    "what is the capital of France", "write me a haiku about the sea",
    "is go installed on this machine", "what time is it", "explain kubernetes to me",
    "why did my pod crash", "which pod is using the most memory",
    "something is wrong with the cluster", "the app is slow, what do i do",
    "set CONSOLE_WORKFLOW_REDESIGN to true in {app}", "edit the configmap for {app}",
    "delete the {res} in {ns}", "how is the cluster doing", "help me debug everything",
    "is everything ok", "what should i look at first", "scale up {app}",
    "create a new namespace", "apply this yaml", "how do i use kubectl",
]


def fill(t, app, res, ns, env):
    res_alias = random.choice(RESOURCE_ALIASES[res]) if res else ""
    return t.format(app=app, res=res_alias, ns=ns, env=env)


def make_sample(intent):
    app = random.choice(APPS)
    ns = random.choice(NAMESPACES)
    env = random.choice(ENV_VARS)
    res = random.choice(RESOURCES) if intent == "list" else ""
    t = random.choice(TEMPLATES[intent])
    # "list" sometimes targets a namespace instead of an app.
    if intent == "list" and "{ns}" in t:
        text = fill(t, app, res, ns, env)
        selector = ns
    else:
        text = fill(t, app, res, ns, env)
        selector = app
    label = {"intent": intent, "app": "", "resource": "", "env_var": ""}
    if intent == "list":
        label["app"] = selector
        label["resource"] = res
    elif intent in ("logs", "image", "rollout", "restart"):
        label["app"] = app
    elif intent == "verifyenv":
        label["app"] = app
        label["env_var"] = env
    return text, label


def make_none():
    app = random.choice(APPS)
    ns = random.choice(NAMESPACES)
    res = random.choice(RESOURCES)
    t = random.choice(NONE_TEMPLATES)
    text = t.format(app=app, ns=ns, res=random.choice(RESOURCE_ALIASES[res]))
    return text, {"intent": "none", "app": "", "resource": "", "env_var": ""}


def to_row(text, label):
    return {"messages": [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": "Request: " + text},
        {"role": "assistant", "content": json.dumps(label)},
    ]}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out-dir", default=".")
    ap.add_argument("--n", type=int, default=3000, help="total positive samples")
    ap.add_argument("--none-frac", type=float, default=0.25, help="fraction that are 'none'")
    ap.add_argument("--eval-frac", type=float, default=0.1)
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    random.seed(args.seed)
    intents = list(TEMPLATES.keys())
    rows = []
    n_none = int(args.n * args.none_frac)
    n_pos = args.n - n_none
    for i in range(n_pos):
        rows.append(to_row(*make_sample(intents[i % len(intents)])))
    for _ in range(n_none):
        rows.append(to_row(*make_none()))
    random.shuffle(rows)

    # Dedup identical (text,label) rows so the eval split is honest.
    seen, uniq = set(), []
    for r in rows:
        key = (r["messages"][1]["content"], r["messages"][2]["content"])
        if key in seen:
            continue
        seen.add(key)
        uniq.append(r)

    n_eval = int(len(uniq) * args.eval_frac)
    eval_rows, train_rows = uniq[:n_eval], uniq[n_eval:]

    os.makedirs(args.out_dir, exist_ok=True)
    train_p = os.path.join(args.out_dir, "router_dataset_train.jsonl")
    eval_p = os.path.join(args.out_dir, "router_dataset_eval.jsonl")
    with open(train_p, "w", encoding="utf-8") as f:
        for r in train_rows:
            f.write(json.dumps(r) + "\n")
    with open(eval_p, "w", encoding="utf-8") as f:
        for r in eval_rows:
            f.write(json.dumps(r) + "\n")
    print(f"wrote {len(train_rows)} train + {len(eval_rows)} eval rows "
          f"({len(uniq)} unique of {len(rows)} generated)")
    print(f"  {train_p}\n  {eval_p}")


if __name__ == "__main__":
    main()

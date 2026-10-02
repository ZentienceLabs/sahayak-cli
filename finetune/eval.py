#!/usr/bin/env python3
"""
Score a router model on the held-out eval split — the EVAL-AGAINST-BASE GATE.

This is the most important file in the folder. Your own prior router fine-tune scored
WORSE than the base model (15/22 vs 19/22) — catastrophic forgetting. So NEVER ship a
fine-tune without proving it beats the base on a held-out set. Run this twice:

    # 1) score the BASE model
    python eval.py --model qwen3:4b-instruct --data router_dataset_eval.jsonl

    # 2) score the FINE-TUNE (after importing the GGUF into Ollama — see README)
    python eval.py --model sahayak-router --data router_dataset_eval.jsonl

Ship the fine-tune ONLY if its total accuracy clearly exceeds the base's. If it doesn't,
the deterministic playbooks + semantic router are already doing the job — don't ship it.

It talks to Ollama (the CLI's backend) with the SAME system prompt and JSON-format
constraint the CLI uses, so the score reflects real CLI behavior. Pure stdlib.
"""

import argparse
import json
import urllib.request

INTENT_ENUM = ["list", "logs", "image", "rollout", "restart", "verifyenv", "none"]
FORMAT_SCHEMA = {
    "type": "object",
    "properties": {
        "intent": {"type": "string", "enum": INTENT_ENUM},
        "app": {"type": "string"},
        "resource": {"type": "string"},
        "env_var": {"type": "string"},
    },
    "required": ["intent"],
}


def classify(endpoint, model, system, user):
    body = json.dumps({
        "model": model,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
        "stream": False,
        "format": FORMAT_SCHEMA,
        "options": {"temperature": 0},
    }).encode()
    req = urllib.request.Request(endpoint + "/api/chat", data=body,
                                headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=300) as r:
        resp = json.loads(r.read())
    return resp["message"]["content"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True)
    ap.add_argument("--data", default="router_dataset_eval.jsonl")
    ap.add_argument("--endpoint", default="http://127.0.0.1:11434")
    args = ap.parse_args()

    rows = [json.loads(l) for l in open(args.data, encoding="utf-8") if l.strip()]
    n = len(rows)
    intent_ok = slot_ok = full_ok = parse_fail = 0
    confusion = {}

    for i, row in enumerate(rows):
        system = row["messages"][0]["content"]
        user = row["messages"][1]["content"]
        gold = json.loads(row["messages"][2]["content"])
        try:
            pred = json.loads(classify(args.endpoint, args.model, system, user))
        except Exception:
            parse_fail += 1
            pred = {}
        gi, pi = gold.get("intent"), str(pred.get("intent", "")).strip().lower()
        if gi == pi:
            intent_ok += 1
        else:
            confusion[f"{gi}->{pi or '?'}"] = confusion.get(f"{gi}->{pi or '?'}", 0) + 1
        # Slots count only for the fields this intent uses.
        slots_match = (
            str(pred.get("app", "")).strip() == gold.get("app", "")
            and str(pred.get("resource", "")).strip() == gold.get("resource", "")
            and str(pred.get("env_var", "")).strip() == gold.get("env_var", "")
        )
        if slots_match:
            slot_ok += 1
        if gi == pi and slots_match:
            full_ok += 1
        if (i + 1) % 25 == 0:
            print(f"  {i+1}/{n}…", flush=True)

    print(f"\n=== {args.model} on {args.data} ({n} examples) ===")
    print(f"  intent accuracy : {intent_ok}/{n}  ({100*intent_ok/n:.1f}%)")
    print(f"  slot  accuracy  : {slot_ok}/{n}  ({100*slot_ok/n:.1f}%)")
    print(f"  FULL (intent+slots): {full_ok}/{n}  ({100*full_ok/n:.1f}%)   <-- compare THIS")
    if parse_fail:
        print(f"  parse failures  : {parse_fail}")
    if confusion:
        top = sorted(confusion.items(), key=lambda kv: -kv[1])[:8]
        print("  top intent confusions: " + ", ".join(f"{k}×{v}" for k, v in top))


if __name__ == "__main__":
    main()

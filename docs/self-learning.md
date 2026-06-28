# Self-learning

Sahayak gets better with use — **safely**. It records what happened, judged by
**deterministic** signals (command exit codes, routing hits), and turns repeated patterns
into **suggestions you approve**. It never changes its own behavior, and it never lets the
model judge whether something "worked" (that would poison it).

## What it observes
- **routed** — a cartridge ran; did the command succeed?
- **adhoc** — you ran a command yourself via `!`; did it succeed?
- **missed** — a request that matched no tool.

All appended to `~/.sahayak/learn.jsonl`. It only observes — behavior is unchanged.

## See suggestions
```sh
sahayak learn suggest
```
Three kinds of draft (only patterns that repeat ≥ twice surface, so one-offs aren't noise):

| Mark | Kind | Meaning |
|---|---|---|
| ✚ | `promote-template` | a command you ran successfully a lot → templatize it into a cartridge |
| ⚠ | `fix-template` | a cartridge intent that keeps failing → review/fix its template |
| ○ | `cover-gap` | requests nothing matched → add a phrasing or template to cover them |

## Promote a learned command (human-gated)
You supply the decisions a model shouldn't make — the intent name and the natural-language
phrasing(s). The command defaults to your most-succeeded ad-hoc command; its risk tier is
classified automatically. It's written to a **dynamic overlay cartridge** (default `learned`)
— your shipped/built-in cartridges are never modified.

```sh
# after running `! kubectl get ns` a few times:
sahayak learn promote --intent list-namespaces --phrase "list namespaces,show all namespaces"
# now `sahayak ask "show all namespaces"` routes to it and runs

# or capture an explicit command into a named overlay:
sahayak learn promote --intent top-pods \
  --phrase "what's using cpu" \
  --command "kubectl top pods -A" \
  --cartridge ops
```

Flags: `--intent` (required), `--phrase` (required, comma-separated), `--cartridge` (overlay,
default `learned`), `--command` (default: most-succeeded ad-hoc command).

## Reset
```sh
sahayak learn forget        # clears the observation log (alias clear, reset)
```

## The principle
> Static base (built-in/installed cartridges) is frozen and trusted. The learning loop
> writes only to a separate dynamic overlay, and only after a human approves. The judge of
> success is always deterministic — never the model.

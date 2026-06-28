# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for security problems.** Report privately via
GitHub **Security → Report a vulnerability** (Private Security Advisories) on this
repository. We aim to acknowledge within a few business days.

Include: affected version/commit, repro steps, impact, and any suggested fix.

## Supported versions

The latest `main` and the most recent tagged release receive fixes. Older tags are
best-effort.

## Threat model notes (read if you're auditing)

Sahayak runs commands, so a few properties are load-bearing:

- **The model never authors a command string or decides risk.** Commands come from
  human-authored, deterministic templates; risk is classified in Go; mutating/destructive
  steps require explicit approval. A compromised/weak model cannot escalate on its own.
- **No shell-injection surface.** Commands run via `os/exec` with structured args — never
  `sh -c`.
- **Secrets** are redacted before reaching the model or logs; Sahayak never reads secret
  stores itself.
- **Cartridge supply chain.** Cartridges carry executable command templates, so installs are
  verified: a **SHA-256 checksum** (integrity) and an optional **ed25519 signature** against
  locally **trusted** publisher keys (authenticity). Installs print the declared commands and
  risk tiers for review. Set `SAHAYAK_REQUIRE_SIGNED=1` to refuse unsigned cartridges. Only
  add registries and `trust` keys you actually trust.

If you find a way to bypass the approval gate, the risk classifier, redaction, or the
cartridge verification, that's a security issue — please report it privately as above.

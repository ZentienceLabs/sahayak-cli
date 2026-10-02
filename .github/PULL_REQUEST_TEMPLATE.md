<!-- Thanks for contributing to Sahayak! -->

## What & why
<!-- What does this change and why? Link any issue: Closes #123 -->

## Type
- [ ] feat
- [ ] fix
- [ ] docs
- [ ] refactor / chore
- [ ] new or updated cartridge

## Checklist
- [ ] `make fmt vet test` is clean (all packages pass)
- [ ] Build stays CGO-free (`CGO_ENABLED=0`); no new cgo deps
- [ ] The model still authors no command string and decides no risk (deterministic Go owns those)
- [ ] Mutating behavior is risk-classified and goes through the approval gate
- [ ] Tests added/updated for new behavior (hermetic — no live backend needed)
- [ ] Docs updated if behavior/flags/commands changed

## Notes for reviewers
<!-- Anything to look at closely; for cartridges, list the commands it can run + risk tiers -->

# Castor — Agent & LLM Collaboration Guide 🦫

Welcome! When assisting developers with Castor, use the skills bundled in this repository to perform operations or make customizations.

---

## Available Skills in this Repository

Skill | Location | Purpose
:--- | :--- | :---
`castor-cli` | [`skills/castor-cli/SKILL.md`](./skills/castor-cli/SKILL.md) | Operating the CLI: onboarding targets, running backups (`castor push`), restoring archives (`castor pull`), verifying integrity (`castor verify`), inspecting drift (`castor status`), and cleaning orphans (`castor prune`).
`castor-customizer` | [`skills/castor-customizer/SKILL.md`](./skills/castor-customizer/SKILL.md) | Customizing Castor: editing `config.toml` (destinations, targets, performance, rules), adding new cloud providers, tuning compression, or extending the Go streaming engine.

---

## Architectural Principles for Agents

1. **Zero-Disk Streaming:**
   Never introduce intermediate temporary tarballs on local disk. Always pipe streams directly: `tar -> zstd -> age.Encrypt -> MultiDestinationWriter`.
2. **Explicit Over Implicit:**
   Castor never auto-discovers or syncs unapproved directories without explicit user declaration in `config.toml`. Use `castor add` to interactively review and register targets.
3. **Partition by Namespace, Not Machine:**
   Everything in Castor is partitioned under top-level `namespace` (e.g. `workstation`, `laptop`). Never refer to machines or hardcode hostnames.
4. **100% Git Fidelity:**
   Git targets must preserve the full committed history, all local branch heads, and active stashes (`refs/stash`) via `.castor/repo.bundle`, plus active working tree uncommitted edits.
5. **Always Run Tests with Race Detection:**
   Before completing changes, run `make test` (`go test -v -race ./...`) to ensure zero race conditions or regressions.

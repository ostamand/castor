---
name: castor-cli
description: >-
  Operate, inspect, backup, restore, verify, and troubleshoot with the Castor cold-storage archiver CLI (`castor`).
  Use this skill whenever interacting with Castor vaults, discovering and adding targets, creating or restoring archives,
  verifying in-memory decryptability and SHA-256 integrity, managing cloud destinations (GCS, Google Drive, local NAS),
  or diagnosing system health.
---

# Castor CLI Operations Guide

`castor` is an intentional, developer-first cold-storage vault and multi-cloud streaming archiver. It streams encrypted, compressed archives directly to remote storage without staging intermediate tarballs on local disk.

--------------------------------------------------------------------------------

## Command Cheat Sheet

Command | Description | Common Flags
:--- | :--- | :---
`castor init` | Interactive onboarding wizard to setup namespace, storage, and Age keys | `--config, -c`
`castor add <path>` | Interactively discover and register child projects into `config.toml` | `-r`, `-g`, `-y`, `-n`, `--prefix`
`castor status` | Show target drift, last sync timestamps, systemd timer health, orphans | `--json`, `--no-tui`
`castor push [target]` | Stream changed targets directly to cloud destinations | `-n`, `-f`, `-w <N>`, `--no-tui`
`castor pull [target]` | Decrypt and reconstitute an archive into local workspace | `--to <dir>`, `-k <key>`, `-f`, `--dest <name>`
`castor verify [target]` | In-memory stream decryption & ciphertext SHA-256 validation | `-k <key>`, `--dest <name>`
`castor ls` | List remote archives, byte sizes, and timestamps | `-s <ns>`, `--all-namespaces`, `--dest <name>`
`castor prune` | Garbage-collect remote archives no longer registered in config | `-n`, `-y`
`castor doctor` | Comprehensive health check of tools, Age keys, timer, and providers | `--config, -c`
`castor auth` | Manage Google Cloud and Google Drive OAuth credentials | `login`, `status`, `logout`

--------------------------------------------------------------------------------

## Common Operational Workflows

### 1. Onboarding a Workspace with Multiple Projects
To scan a directory (e.g. `~/Work/git`) and register all Git repositories:
```bash
# Preview what will be found without modifying config.toml:
castor add ~/Work/git -r --git-only --dry-run

# Non-interactive batch registration:
castor add ~/Work/git -r --git-only --yes

# Or launch interactive TUI checklist to pick projects:
castor add ~/Work/git -r --git-only
```
*Note: Build junk (`node_modules`, `.venv`, `target`, `build`) is automatically pruned. Git repositories are treated as atomic units (their internal subdirectories are never split into separate targets).*

### 2. Inspecting and Running Backups (`castor push`)
```bash
# Check status and drift across all targets:
castor status

# Dry-run to see which targets changed:
castor push -n

# Run live backup with interactive Charm TUI dashboard:
castor push

# Run in headless environments (cron, systemd, CI) without TUI animations:
castor push --no-tui

# Push only a specific target:
castor push rollmind

# Force upload ignoring fingerprint cache:
castor push rollmind --force
```

### 3. Restoring Archives (`castor pull`)
Castor guarantees **100% Git fidelity**: restores full committed history, all local branch heads, and active stashes (`refs/stash`), plus active uncommitted/untracked files.
```bash
# Interactive fuzzy-search archive picker:
castor pull

# Restore a project by name into its original configured path:
castor pull rollmind --key $AGE_KEY

# Restore into a custom target directory:
castor pull rollmind --to /tmp/restored-app --key $AGE_KEY

# Overwrite existing files without collision prompt:
castor pull rollmind --to /tmp/restored-app --key $AGE_KEY --force

# Restore from a specific remote namespace:
castor pull rollmind --namespace laptop --key $AGE_KEY
```

### 4. Integrity & Decryptability Verification (`castor verify`)
Streams the remote archive into memory, decrypts on-the-fly, verifies ciphertext SHA-256 against the encrypted sidecar manifest (`.meta.json.age`), and inspects tar headers without writing anything to disk:
```bash
# Verify all targets:
castor verify --key $AGE_KEY

# Verify a single target:
castor verify rollmind --key $AGE_KEY
```

### 5. Cleaning Orphaned Archives (`castor prune`)
When a target is removed from `config.toml`, its cloud archive becomes orphaned:
```bash
# Inspect orphaned archives across all destinations:
castor prune --dry-run

# Delete orphaned archives without confirmation prompts:
castor prune --yes
```

--------------------------------------------------------------------------------

## Environment Variables

Variable | Description
:--- | :---
`CASTOR_AGE_KEY` | Age private secret key (`AGE-SECRET-KEY-1...`). Avoids passing `-k` on CLI.
`CASTOR_CONFIG` | Alternate path to `config.toml` (overridden by `-c / --config`).
`GOOGLE_APPLICATION_CREDENTIALS` | Service account key path for Google Cloud Storage (GCS).

--------------------------------------------------------------------------------

## Troubleshooting & Failure Modes

Symptom | Probable Cause | Resolution
:--- | :--- | :---
`circuit breaker tripped: archive size exceeds ceiling` | Target compressed size exceeds `max_archive_size_gb`. | Adjust `max_size_gb` in `[[targets]]` or `max_archive_size_gb` in `[safety]`.
`unknown Age recipient / private key required` | Backup is encrypted but no private key was provided for restore/verify. | Provide private key via `-k <key>` or export `CASTOR_AGE_KEY=AGE-SECRET-KEY-1...`.
`destination directory already exists` | `castor pull` guards against accidental overwrites. | Use `-f / --force` to overwrite existing files, or specify `--to <new-dir>`.
`not a git repository` | Target configured with `type = "git"` is missing `.git`. | Change `type = "generic"` in `config.toml` or initialize Git.
`refusing to fetch into checked-out branch` | Older version of Castor during pull reconstitution. | Reconstituted using `git clone --mirror` in Castor v0.4.0+. Upgrade with `make install`.

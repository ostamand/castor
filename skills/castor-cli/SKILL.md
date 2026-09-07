---
name: castor-cli
description: >-
  Operate, inspect, backup, restore, verify, and troubleshoot with the Castor cold-storage archiver CLI (`castor`).
  Use this skill whenever interacting with Castor archives, discovering and adding targets, creating or restoring archives,
  verifying in-memory decryptability and SHA-256 integrity, managing cloud destinations (GCS, Google Drive, local NAS),
  or diagnosing system health.
---

# Castor CLI Operations Guide

`castor` is an intentional, developer-first cold-storage and multi-cloud streaming archiver. It streams encrypted, compressed archives directly to remote storage without staging intermediate tarballs on local disk.

--------------------------------------------------------------------------------

## Command Cheat Sheet

Command | Description | Common Flags
:--- | :--- | :---
`castor init` | Interactive onboarding wizard to setup namespace, storage, and Age keys | `--config, -c`
`castor add <path>` | Register project or scan child directories for targets | `-r`, `-g`, `-y`, `-n`, `--name`, `--prefix`
`castor remove <target>` | Remove registered target from config.toml (`--purge` to delete cloud archives) | `-y`, `--purge` (alias: `rm`)
`castor provider [cmd]` | Manage storage destinations: list, add, remove, and test reachability | `list`, `add`, `remove`, `test` (alias: `destination`)
`castor config [edit]` | View configuration or open in `$EDITOR` with post-save validation & warnings | `edit`
`castor status` | Show target drift, last sync timestamps, timer health, and last run outcome | `--json`, `--no-tui`
`castor diff [target]` | Compare local workspace against remote archive manifest without downloading archive | `-k <key>`, `-d <dest>`, `--json`
`castor inspect <target>` | Inspect archive contents in-memory without extracting to disk | `-k <key>`, `-p <pattern>`, `--json`
`castor cat <target> <file>` | Stream single file directly from remote archive to stdout or `--out` | `-k <key>`, `-o <file>`
`castor push [target]` | Stream changed targets directly to cloud destinations | `-n`, `-f`, `-w <N>`, `--no-tui`
`castor pull [target]` | Decrypt and reconstitute an archive into local workspace | `--to <dir>`, `-k <key>`, `-f`, `--dest <name>`
`castor verify [target]` | In-memory stream decryption & ciphertext SHA-256 validation | `-k <key>`, `--dest <name>`
`castor ls` | List remote archives, byte sizes, and timestamps | `-s <ns>`, `--all-namespaces`, `--dest <name>`
`castor prune` | Garbage-collect remote archives no longer registered in config | `-n`, `-y`
`castor schedule [cmd]` | Manage automated background backup timer, inspect run history & logs | `enable`, `disable`, `status`, `history`, `logs`, `run`
`castor doctor` | Comprehensive health check of tools, Age keys, timer, and providers | `--config, -c`
`castor auth` | Manage Google Cloud, Google Drive, and Dropbox OAuth credentials | `login`, `status`, `logout`
`castor uninstall` | Completely uninstall Castor binary, timers, and skills | `-y`, `--purge`

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

### 1b. Renaming Targets (`castor mv` / `castor rename`)
Renames an existing target in `config.toml`, migrates local state tracking in `state.json`, and stream-renames remote cloud archives and sidecar metadata across all active destinations without re-uploading:
```bash
# Rename target:
castor mv modo git/modo

# Using alias:
castor rename git/modo personal/modo

# Dry-run preview:
castor mv modo git/modo --dry-run
```

### 1c. Managing Storage Destinations & Providers (`castor provider` / `castor destination`)
Castor supports zero-disk multi-cloud fan-out streaming to Local NVMe/NAS, Google Drive, Dropbox, and GCS:
```bash
# List all configured destinations:
castor provider list
castor destination

# Add a local directory, external SSD, or NAS mount:
castor provider add local /mnt/nas/castor --name local-nas
castor provider add local ~/Backups/castor

# Add a Dropbox destination (defaults to root of App folder):
castor provider add dropbox --name my-dropbox

# Add a Google Drive destination:
castor provider add gdrive --folder CastorLodge --name google-drive

# Add a Google Cloud Storage bucket:
castor provider add gcs --bucket my-castor-coldline --location northamerica-northeast1

# Interactive wizard (prompts for provider, name, and settings):
castor provider add

# Test connectivity and write permissions:
castor provider test
castor provider test my-dropbox

# Remove a destination:
castor provider remove local-nas -y
```

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

### 4. Remote Drift Comparison (`castor diff`)
Compares active local workspaces against remote sidecar metadata manifests (`.meta.json.age`) without downloading or extracting the archive:
```bash
# Compare all configured targets:
castor diff --key $AGE_KEY

# Compare a specific target:
castor diff rollmind --key $AGE_KEY

# Output structured JSON:
castor diff rollmind --json --key $AGE_KEY
```

### 5. Inspecting Archive Contents (`castor inspect`)
Decompresses and decrypts the archive stream directly in-memory to view the file table without local extraction:
```bash
# List all files in the remote archive:
castor inspect rollmind --key $AGE_KEY

# Filter by glob pattern:
castor inspect rollmind --pattern "*.go" --key $AGE_KEY
castor inspect rollmind --pattern "config/*" --key $AGE_KEY

# Output archive manifest as JSON:
castor inspect rollmind --json --key $AGE_KEY
```

### 6. Streaming Single Files (`castor cat`)
Streams a specific file directly from the remote archive to `stdout` or an output path without extracting the rest of the archive, immediately closing the stream once transferred:
```bash
# Stream file to stdout:
castor cat rollmind README.md --key $AGE_KEY

# Pipe directly to another command or viewer:
castor cat rollmind config/app.toml --key $AGE_KEY | jq .

# Extract file to disk:
castor cat rollmind config.local.json --out ./config.local.json --key $AGE_KEY
```

### 7. Integrity & Decryptability Verification (`castor verify`)
Streams the remote archive into memory, decrypts on-the-fly, verifies ciphertext SHA-256 against the encrypted sidecar manifest (`.meta.json.age`), and inspects tar headers without writing anything to disk:
```bash
# Verify all targets:
castor verify --key $AGE_KEY

# Verify a single target:
castor verify rollmind --key $AGE_KEY
```

### 8. Cleaning Orphaned Archives (`castor prune`)
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
`refusing to fetch into checked-out branch` | Older version of Castor during pull reconstitution. | Reconstituted using `git clone --mirror` in Castor v0.1.0+. Upgrade with `make install`.

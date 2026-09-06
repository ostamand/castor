# Castor 🦫

> Nature's engineer, lodge builder, and cold-storage vault architect.

**Castor** is an intentional, developer-first cold-storage vault and multi-cloud streaming archiver written in Go. It captures, packages, encrypts, and streams explicit project workspaces directly into cost-effective cloud object storage without local staging or continuous background resource overhead.

---

## Key Features

* **Per-Namespace Partitioning:** Organizes archives by top-level namespace (e.g. `workstation`, `laptop`, `work`). Multiple machines safely share the same GCS bucket or Google Drive folder with **zero risk of collision**.
* **Zero-Disk Streaming:** Streams `tar -> zstd -> age.Encrypt` in-memory directly to cloud HTTP/2 writers via `io.MultiWriter`. No multi-gigabyte scratch archives staged on your SSD.
* **Write-Only Host Security:** Asymmetric `age` encryption (`age1...` public key stored on host; private key stays offline). Past backups cannot be decrypted if the workstation is compromised.
* **100% Git Preservation:** Captures full committed history (`.castor/repo.bundle` including `refs/stash`) + active working tree uncommitted edits in a single unified container archive.
* **First-Class Storage Backends:**
  * **Google Cloud Storage (GCS):** Automatic Nearline/Coldline/Archive lifecycle management ($0.004/GB/mo).
  * **Google Drive:** Zero-knowledge streaming directly to a designated folder via Google Drive REST API v3 using personal Google One / Workspace quota.
  * **Local Filesystem / NAS / External Drive:** Stream directly to local mount points, external backup drives, or local NAS (`provider = "local"`).
* **Interactive Terminal UI:** Built with the Charm ecosystem (`bubbletea`, `huh`, `lipgloss`, `bubbles`) featuring split-pane checklists, live streaming progress bars, and fuzzy-search archive explorers (with automatic non-TTY fallback for systemd/cron).
* **Defensive Live Archiving:** Bounded file reads (`copyWithBound`) prevent stream crashes when working on live projects with active background compilers or shifting files.
* **In-Memory Verification (`castor verify`):** Validates archive decryptability and SHA-256 integrity against encrypted sidecars without unpacking to disk.

---

## Installation

### From Source
```bash
git clone https://github.com/ostamand/castor.git
cd castor
make install
```

### Self-Upgrade
```bash
castor upgrade
```

---

## Quick Start

### 1. Guided Setup Wizard
Run the interactive onboarding wizard to configure your namespace, cloud backends, and Age keys:
```bash
castor init
```

### 2. Discover & Register Projects
Recursively scan and register all Git repositories under a parent directory:
```bash
castor add ~/projects -r --git-only
```

### 3. Inspect Backup Plan (Dry Run)
Inspect what changed and what will be streamed to the cloud:
```bash
castor push -n
```

### 4. Push to Cloud Vault
Stream changed targets to all configured cloud destinations:
```bash
castor push
```

### 5. Restore an Archive
Launch the interactive fuzzy-search archive picker:
```bash
castor pull
```
Or restore directly by target name or custom destination:
```bash
# Restore to original workspace:
castor pull rollmind

# Or restore to a custom location:
castor pull rollmind --to /tmp/restored-project --key $AGE_KEY
```

---

## CLI Command Reference

### `castor init`
Guided onboarding wizard to configure namespace, cloud providers, and Age encryption.
* **Flags:**
  * `--config, -c <path>`: Path to config file (default: `~/.config/castor/config.toml`).

### `castor add <path> [flags]`
Interactively discovers and registers child folders into `config.toml`.
* **Positional Arguments:**
  * `<path>` *(required, string)*: Parent directory to scan.
* **Flags:**
  * `-r, --recursive` *(optional, bool)*: Traverse subdirectories recursively.
  * `-g, --git-only` *(optional, bool)*: Restrict discovery strictly to Git repositories.
  * `--max-depth <int>` *(optional, int)*: Maximum recursion depth (default: 1 without `-r`, 4 with `-r`).
  * `--prefix <string>` *(optional, string)*: Prepend a prefix to discovered target names.
  * `--type <string>` *(optional, string)*: Default type for non-Git directories (`generic`, `documents`, `media`).
  * `-y, --yes` *(optional, bool)*: Non-interactive mode; add all valid candidates without prompting.
  * `-n, --dry-run` *(optional, bool)*: Print discovered candidates without modifying `config.toml`.

### `castor push [target] [flags]`
Streams changed targets directly to all active cloud destinations.
* **Positional Arguments:**
  * `[target]` *(optional, string)*: Specific target name or path to push (pushes all changed targets if omitted).
* **Flags:**
  * `-n, --dry-run` *(optional, bool)*: Simulate push without uploading data.
  * `-f, --force` *(optional, bool)*: Force push all targets ignoring fingerprint cache.
  * `-w, --workers <int>` *(optional, int)*: Number of concurrent upload workers.

### `castor pull [target] [flags]`
Decrypts and reconstitutes an archive into your workspace.
* **Positional Arguments:**
  * `[target]` *(optional, string)*: Archive key to restore (launches interactive picker if omitted).
* **Flags:**
  * `-s, --namespace <string>` *(optional, string)*: Namespace to restore from.
  * `--to <string>` *(optional, string)*: Restore into a custom directory path.
  * `-f, --force` *(optional, bool)*: Overwrite existing local files without prompt.
  * `--dest <string>` *(optional, string)*: Specific destination to pull from.
  * `-k, --key <string>` *(optional, string)*: Age secret key (or set `CASTOR_AGE_KEY`).

### `castor ls [flags]`
Lists remote cloud archives, byte sizes, and timestamps.
* **Flags:**
  * `-s, --namespace <string>` *(optional, string)*: List archives for a specific namespace.
  * `--all-namespaces` *(optional, bool)*: List archives across all namespaces in the vault.
  * `--dest <string>` *(optional, string)*: Filter to a specific destination provider.
  * `-i, --interactive` *(optional, bool)*: Launch interactive archive browser.

### `castor verify [target] [flags]`
Streams archive down, tests in-memory Age decryption, and validates SHA-256 against sidecar manifest.
* **Flags:**
  * `-s, --namespace <string>` *(optional, string)*: Namespace to verify.
  * `--dest <string>` *(optional, string)*: Destination to verify against.
  * `-k, --key <string>` *(optional, string)*: Age secret key (or set `CASTOR_AGE_KEY`).

### `castor status`
Displays drift between local targets and local state cache, systemd timer health, and orphaned archives.

### `castor diff [target] [flags]`
Compares active local workspaces against remote sidecar metadata manifests (`.meta.json.age`) in the vault without downloading the full archive.
* **Flags:**
  * `-k, --key <string>`: Age secret key (or export `CASTOR_AGE_KEY`).
  * `-d, --dest <string>`: Destination to compare against.
  * `-s, --namespace <string>`: Target namespace to inspect.
  * `--json`: Emit machine-readable JSON drift summary.

### `castor inspect <target> [flags]`
Decompresses and decrypts the remote archive stream directly in memory, displaying the full file table with sizes and permissions without extracting to disk.
* **Aliases:** `view`, `info`
* **Flags:**
  * `-k, --key <string>`: Age secret key (or export `CASTOR_AGE_KEY`).
  * `-p, --pattern <string>`: Filter file paths by glob pattern (e.g. `*.go`, `config/*`).
  * `-d, --dest <string>`: Specific destination to inspect.
  * `-s, --namespace <string>`: Namespace to inspect.
  * `--json`: Emit archive manifest and entry list as JSON.

### `castor cat <target> <file-path> [flags]`
Streams a single file directly from the remote archive to `stdout` or an output path and immediately aborts the stream once transferred.
* **Flags:**
  * `-k, --key <string>`: Age secret key (or export `CASTOR_AGE_KEY`).
  * `-o, --out <path>`: Write output to a local file instead of standard output.
  * `-d, --dest <string>`: Specific destination to read from.
  * `-s, --namespace <string>`: Namespace to inspect.

### `castor prune [flags]`
Garbage-collects cloud archives that were removed from `config.toml`.
* **Flags:**
  * `-n, --dry-run` *(optional, bool)*: List orphaned archives without deleting.
  * `-y, --yes` *(optional, bool)*: Confirm deletion without interactive prompt.

### `castor auth [login|status|logout]`
Manages Google Cloud Storage and Google Drive OAuth credentials.

### `castor doctor`
Diagnoses system tools (`git`), systemd timers, Age keys, and cloud reachability.

---

## Configuration Reference (`~/.config/castor/config.toml`)

```toml
namespace = "workstation"   # Vault-scoped storage partition

[performance]
max_workers = 4             # Concurrent upload workers
compression_level = 19      # zstd level (1-22, 19 = optimal cold storage)

[safety]
max_archive_size_gb = 5.0   # Circuit breaker ceiling per target

[security]
encrypt = true
encryption_method = "age"
age_public_keys = [
    "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
]

[[destinations]]
name = "gcp-coldline"
provider = "gcs"
bucket = "castor-vault-montreal"
location = "northamerica-northeast1"
prefix = "archives"

[[destinations]]
name = "gdrive-mirror"
provider = "gdrive"
folder = "CastorLodge/archives"

[[destinations]]
name = "backup-disk"
provider = "local"
path = "/mnt/backup/castor-vault"

[[targets]]
name = "rollmind"
path = "~/projects/rollmind"
type = "git"
compression = "zstd"
create_git_bundle = true

[[targets]]
name = "documents-vault"
path = "~/Documents/Vault"
type = "documents"
compression = "xz"

[[targets]]
name = "caddy"
path = "/etc/caddy"
type = "documents"
compression = "zstd"

[rules.git]
excludes = [
    ".git", "node_modules", ".npm", ".yarn", ".pnpm-store",
    ".next", ".nuxt", ".turbo", "dist", "build", "out",
    "__pycache__", "*.pyc", ".venv", "venv", "env",
    "target", "bin", "obj", "*.log", ".DS_Store"
]
```

---

## Testing & Quality Assurance

Castor includes a comprehensive regression and end-to-end integration test suite executed with Go's race detector enabled:

```bash
# Run all unit, regression, and full CLI integration tests
make test

# Compile binary into bin/castor
make build
```

### Key Areas Tested
* **Full CLI Lifecycle Integration (`cli_integration_test.go`):** Exercises the complete sequence (`doctor` → `add` → `status` → `push` → `ls` → `verify` → `pull` → `prune`) in an isolated sandbox.
* **100% Git Reconstitution (`pipeline_restore_test.go`):** Validates that `.castor/repo.bundle` reconstitutes the entire committed history, all local branch heads, active stashes (`git stash list` & `git stash pop`), and uncommitted working-tree edits.
* **Live Shifting-File Safety (`archive_test.go`):** Verifies that `copyWithBound` clamps files that grow dynamically during tar streaming and zero-pads shrinking/vanished files without corrupting the tar stream.
* **Microsecond Tree Drift (`fingerprint_test.go`):** Tests state transitions across clean commits, unstaged changes, staged additions, untracked files, and stashes to guarantee zero false transfers.
---

## LLM & Agent Skills (Customization via AI)

Castor ships with built-in agent skills stored inside the repository (`skills/`) to allow LLMs and autonomous coding assistants to safely operate, customize, and extend Castor:

| Skill | Path | Description |
| :--- | :--- | :--- |
| `castor-cli` | [`skills/castor-cli/SKILL.md`](./skills/castor-cli/SKILL.md) | Teaches LLMs how to operate the CLI, run backups, restore archives, verify integrity, and inspect drift. |
| `castor-customizer` | [`skills/castor-customizer/SKILL.md`](./skills/castor-customizer/SKILL.md) | Guides LLMs on modifying `config.toml`, adding targets, implementing custom storage providers in Go, and tuning compression. |

To link these skills into your local agent environment (similarly to Omarchy):
```bash
make install-skills
```
This creates symlinks from the repository into `~/.gemini/config/skills/`, ensuring that any updates in the repo are immediately reflected to your LLM assistant.

---

## License

MIT / Apache-2.0

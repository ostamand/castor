# Castor — Technical Specification, Architecture Blueprint & Product Vision

**Project Name:** Castor (`castor`)  
**Mascot & Theme:** *Castor canadensis* (The beaver — nature’s engineer, lodge builder, and winter cache architect)  
**Binary Class:** Standalone CLI tool written in Go  
**Specification Version:** `0.1.0-spec`  
**License Target:** MIT / Apache-2.0  

---

## 1. Vision, Philosophy & Rationale

**Castor** is an intentional, developer-first cold-storage vault and multi-cloud streaming archiver. It is fundamentally engineered around **namespaces**: it captures, packages, encrypts, and streams explicit project workspaces into a dedicated, user-defined cloud namespace without local staging or continuous background resource overhead.

Castor places paramount emphasis on **rich terminal interactivity**: rather than asking developers to memorize arcane flags or edit raw config files by hand, Castor provides an interactive, visually elegant terminal experience (built with Charm’s `bubbletea`, `huh`, and `lipgloss`) with live multi-stream progress bars, fuzzy-search archive pickers, and guided onboarding forms.

### 1.1 Why Castor Exists: Active Sync vs. Cold Preservation

Consumer sync tools (Dropbox, Google Drive desktop client, Nextcloud, OneDrive) are engineered for continuous bi-directional mirroring. For software engineers, this design presents severe friction:

1. **Dependency & Build Cache Bloat:** Modern development generates hundreds of thousands of transient files (`node_modules`, `.venv`, `target/`, `.next`, `__pycache__`, `.turbo`, `dist/`). Continuous sync daemons thrash network bandwidth, exhaust OS inode tables, and hammer SSD read/write cycles uploading files that are trivially regenerated.
2. **Resource Overhead & Battery Drain:** Continuous filesystem watchers (`inotify` on Linux, `FSEvents` on macOS) exhaust system limits, lock files during compiler passes, and prevent mobile CPUs from entering deep C-states. Castor runs only when explicitly invoked (or via a scheduled user timer), executes in milliseconds to seconds, and exits.
3. **Intentionality:** Active workspaces contain uncommitted spikes, broken tests, and partial refactors. Backups should represent deliberate point-in-time milestones with Git bundles and clean working states, decoupled from day-to-day workspace churn.
4. **Zero Proprietary Lock-In:** Chunk-deduplication systems (Borg, Restic, Kopia) fragment files into opaque content-addressed chunk databases. If the metadata database corrupts, recovery is painful. Castor produces standard, transparent Unix artifacts (`.bundle`, `.tar.zst`, `.age`) that can be restored on any Linux/Unix machine with standard coreutils, `git`, and `age` without Castor installed.

### 1.2 Core Architectural Tenets

* **Interactive, Rich Terminal UI:** A delightful developer experience with interactive forms (`huh`), searchable checklists (`bubbletea`), and color-coded status badges (`lipgloss`). If run in a headless environment (cron, systemd timer, CI), Castor automatically detects non-TTY mode and outputs clean, structured plain logs.
* **Namespaces by Default:** Castor organizes vaults into top-level namespaces. On `castor init`, each vault defines its namespace (e.g. `workstation`, `laptop`, `work`, `homelab`). All archives are scoped under `archives/<namespace>/...`. Multiple systems or environments can freely share the same GCS bucket or Google Drive folder with **zero risk of colliding or overwriting each other**.
* **Explicit Over Implicit:** No magical scans or unexpected background uploads. Only directories declared under `[[targets]]` in `~/.config/castor/config.toml` are ever backed up.
* **Write-Only Host Security:** Asymmetric `age` encryption ensures the backup workstation only stores the public recipient key (`age1...`). If the workstation is lost or compromised, historical cloud archives cannot be decrypted without the offline private key.
* **Zero-Disk Streaming:** Compression and encryption occur entirely in memory via Unix-style streaming pipes directly into HTTP/2 cloud writers. Multi-gigabyte scratch archives are never staged on local disks.
* **Microsecond Fingerprinting:** Git repos are fingerprinted via `git rev-parse HEAD + git status --porcelain` hashes, evaluating 50+ repositories in under 500ms. If no changes exist, network transfer is 0 bytes.
* **Safety Circuit Breakers:** Configurable size ceilings (`max_archive_size_gb`) prevent runaway transfer costs before any bytes hit the wire.
* **Root-Aware Origin Paths:** Archive paths under the namespace mirror origin directories under `user/...` (relative to `$HOME`, username-agnostic) or `system/...` (absolute root/mount paths), making restores deterministic and intuitive.
* **Self-Describing Encrypted Sidecars:** Every archive is paired with an encrypted `.meta.json.age` manifest containing host info, namespace, origin paths, Git commit hashes, SHA-256 payload checksums, and timestamps for unambiguous disaster recovery.

---

## 2. CLI Mental Model & Verbs

Castor organizes commands around clear, intuitive actions:

```bash
castor [action] [target] [flags]
```

| Command | Aliases | Purpose | Interactive Behavior |
| :--- | :--- | :--- | :--- |
| `push` | `lodge` | Stream changed targets to cloud destinations | Live multi-worker progress dashboard with transfer speeds and byte counters |
| `pull` | `retrieve` | Decrypt and restore an archive from cloud storage | Interactive fuzzy-search archive picker with namespace filter and metadata preview |
| `add` | `scan` | Interactively discover and register child folders | Interactive multi-select checklist with live size estimation and collision alerts |
| `ls` | `cache` | List remote cloud archives, byte sizes, and timestamps | Formatted Lipgloss table; `-i` launches interactive explorer |
| `status` | `diff` | Show drift between local targets and cloud state | Visual health dashboard showing provider status, timer health, and sync drift |
| `verify` | `check` | In-memory verification of archive integrity | Interactive progress with per-target verification checkmarks |
| `prune` | `gc` | Interactively remove orphaned cloud archives | Interactive checklist of orphaned archives with confirmation modal |
| `init` | — | Guided setup wizard | Step-by-step interactive form (`huh`) for namespace, providers, and Age keys |
| `auth` | — | Manage cloud provider credentials and OAuth loopback | Browser loopback with spinner and token status display |
| `doctor` | — | Validate dependencies, Age keys, reachability | Visual diagnostic checklist with pass/warn/fail badges |
| `upgrade`| — | In-place binary self-updater | Progress bar during binary download and checksum verification |

---

## 3. The Namespace Vault Architecture

### 3.1 Motivation & Hierarchy

Developers frequently work across multiple computers, projects, or distinct scopes:
* An office desktop (`workstation`)
* A portable computer (`laptop`)
* Distinct work domains (`work` vs `personal`)
* A server or homelab (`homelab`)

If these share a common GCS bucket (`gs://my-vault`) or Google Drive folder (`CastorLodge`), a flat storage structure would cause severe collisions whenever two scopes have a project in the same relative path (e.g. `~/projects/rollmind`).

Castor solves this cleanly by making **namespace a primary partition**:
```
CastorLodge/
├── workstation/                   <- Namespace 1
│   ├── user/
│   │   ├── projects/
│   │   │   ├── rollmind.tar.zst.age
│   │   │   └── rollmind.meta.json.age
│   │   └── Documents/
    │   │       └── Vault.tar.xz.age
    │   └── system/
    │       └── etc/caddy.tar.zst.age
    │
    └── laptop/                        <- Namespace 2
        ├── user/
        │   └── projects/
        │       ├── rollmind.tar.zst.age
        │       └── rollmind.meta.json.age
        └── system/
            └── etc/wireguard.tar.xz.age
```

### 3.2 Canonical Cloud Key Derivation with Namespace Scope

For any target, its full remote object path is:

```
archives/<namespace>/<scope>/<relative-path>.<ext>.age
```

#### Derivation Logic
```go
func CanonicalCloudKey(namespace string, targetPath string, explicitSubNamespace string) string {
    namespace = strings.Trim(namespace, "/")
    if explicitSubNamespace != "" {
        return filepath.ToSlash(filepath.Join(namespace, strings.Trim(explicitSubNamespace, "/")))
    }

    cleanPath := filepath.Clean(targetPath)
    home, _ := os.UserHomeDir()
    cleanHome := filepath.Clean(home)

    // User home scope
    if strings.HasPrefix(targetPath, "~/") {
        rel := strings.TrimPrefix(targetPath, "~/")
        return filepath.ToSlash(filepath.Join(namespace, "user", rel))
    }
    if strings.HasPrefix(cleanPath, cleanHome+string(filepath.Separator)) {
        rel := strings.TrimPrefix(cleanPath, cleanHome)
        rel = strings.TrimPrefix(rel, string(filepath.Separator))
        return filepath.ToSlash(filepath.Join(namespace, "user", rel))
    }

    // System root scope
    rel := strings.TrimPrefix(cleanPath, string(filepath.Separator))
    return filepath.ToSlash(filepath.Join(namespace, "system", rel))
}
```

---

## 4. Rich Interactive TUI Architecture

Castor leverages the **Charm ecosystem** (`bubbletea`, `huh`, `lipgloss`, `bubbles`) to deliver an exceptionally polished, human-centered terminal interface.

```
┌─────────────────────────────────────────────────────────────────┐
│                    Castor TUI Architecture                      │
├──────────────────┬─────────────────────────────┬────────────────┤
│  Component       │  Library                    │  Use Case      │
├──────────────────┼─────────────────────────────┼────────────────┤
│  Form Wizard     │  github.com/charmbracelet/huh│  castor init   │
│  Interactive TUI │  .../bubbletea & bubbles    │  add, pull, ls │
│  Styling & Theme │  github.com/charmbracelet/  │  Badges, boxes,│
│                  │  lipgloss                   │  tables, colors│
│  Progress / Spin │  bubbles/progress, spinner  │  push, verify  │
└──────────────────┴─────────────────────────────┴────────────────┘
```

### 4.1 Guided Wizard (`castor init`) with `huh`
`castor init` renders a multi-step interactive form with arrow navigation, real-time input validation, and description hints:

```text
╭────────────────────────────────────────────────────────────────────────────╮
│ 🦫 Castor · Guided Vault Setup                                             │
╰────────────────────────────────────────────────────────────────────────────╯

? Vault Namespace
  Define the namespace for this device or profile.
  > workstation

? Storage Destinations (Select all that apply)
  [•] Google Cloud Storage (GCS) - Ideal for Nearline/Coldline lifecycle rules
  [•] Google Drive               - Uses personal Google One / Workspace quota
  [ ] AWS S3                     - S3 Standard / Glacier Instant Retrieval

? GCS Configuration
  Bucket Name: castor-vault-montreal
  Location:    northamerica-northeast1

? Google Drive Configuration
  Folder:      CastorLodge

? Age Encryption Key
  [X] Generate new write-only Age keypair (Recommended)
  [ ] Use existing Age public key

  [ Next ]      [ Cancel ]
```

### 4.2 Interactive Scanner (`castor add`) with Bubble Tea
When running `castor add ~/projects --recursive --git-only`, Castor displays a split-pane checklist:

```text
🦫 Castor · Discovered 4 Git repositories under ~/projects (namespace: workstation)

  [X] rollmind            (24.2 MB)  │ Target:   user/projects/rollmind
  [X] monster-word-lab    (84.5 MB)  │ Type:     git (bundle + working tree)
  [X] generated-visions   (112.0 MB) │ Est Size: 24.2 MB (zstd ~7.1 MB)
  [ ] legacy-proto        (1.8 MB)   │ Status:   Ready to register
                                     │
[/] Filter · [Space] Toggle · [a] All · [n] None · [Enter] Confirm & Write
```

### 4.3 Interactive Archive Browser (`castor pull`)
Running `castor pull` without arguments launches a fuzzy-searchable archive picker:

```text
🦫 Castor · Cloud Archive Browser (Namespace: workstation)

  Search: > roll_

  Archive Name                  Size      Last Pushed      Git Ref      Status
  ─────────────────────────────────────────────────────────────────────────────
▸ user/projects/rollmind        24.2 MB   2 hours ago      main @ a4f91c  Healthy
  user/projects/rollmind-old    18.1 MB   3 months ago     v1.0 @ e81b2a  Healthy

  ─────────────────────────────────────────────────────────────────────────────
  Origin:      /home/ostamand/projects/rollmind
  Host:        workstation-arch (linux/amd64)
  Cipher:      age-x25519 · SHA-256: e3b0c442...
  Restore to:  ~/projects/rollmind (Original path)

  [Enter] Restore · [Tab] Switch Namespace · [c] Custom Path · [q] Quit
```

### 4.4 Live Streaming Dashboard (`castor push`)
During active pushes, Castor renders a live multi-worker progress dashboard:

```text
🦫 Castor · Pushing changed targets to [gcp-coldline, gdrive-mirror]

  ✔ user/projects/monster-word-lab  (no changes · 0 B transferred)
  ⠋ user/projects/rollmind          [=================>      ] 72%  (17.4 / 24.2 MB · 4.8 MB/s)
  ⠋ user/Documents/Vault            [========>               ] 35%  (4.2 / 12.0 MB · 3.1 MB/s)

  Active Workers: 2/4 · Overall: [===============>        ] 64% · ETA: 4s
```

### 4.5 Graceful Headless Fallback
When Castor detects that stdin/stdout is not a TTY (such as when invoked by `systemd.timer`, `cron`, or CI/CD pipelines):
* ANSI cursor controls, spinners, and interactive prompts are automatically disabled.
* Structured, timestamps log messages or JSON output are produced.
* Prompts default to safe non-destructive fallbacks (failing safe instead of hanging).

---

## 5. Encrypted Sidecar Metadata Manifest (`.meta.json.age`)

### 5.1 Purpose & Security
Every archive (`.tar.zst.age` or `.bundle`) is accompanied by an encrypted sidecar manifest (`.meta.json.age`).
Because the manifest is encrypted with `age` before transmission, cloud providers (including Google Drive and GCS) cannot inspect hostnames, usernames, file paths, or Git commit history.

### 5.2 Manifest Schema (`v1.0`)

```json
{
  "version": "1.0",
  "namespace": "workstation",
  "archive_name": "rollmind.tar.zst.age",
  "canonical_key": "workstation/user/projects/rollmind",
  "created_at": "2026-09-06T18:00:00Z",
  "host": {
    "hostname": "workstation-arch",
    "os": "linux",
    "arch": "amd64",
    "user": "ostamand"
  },
  "origin": {
    "path": "/home/ostamand/projects/rollmind",
    "scope": "user",
    "type": "git"
  },
  "git": {
    "commit": "a4f91c3d987e02b21c45f8a9e7d82b01c38e92fa",
    "branch": "main",
    "has_stash": true,
    "dirty": false,
    "uncommitted_files_count": 0
  },
  "payload": {
    "compression": "zstd",
    "compression_level": 19,
    "encrypted": true,
    "cipher": "age-x25519",
    "uncompressed_bytes": 88592384,
    "archive_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  }
}
```

### 5.3 On-The-Fly SHA-256 Checksum Calculation
Because Castor does not stage archives on disk, the ciphertext SHA-256 hash is calculated in-memory during streaming:

```
Archive Stream -> age.Encrypt -> io.MultiWriter(GCS, GDrive, sha256.New())
```

When the archive stream closes:
1. `archive_sha256 = hex.EncodeToString(hasher.Sum(nil))` is injected into the metadata struct.
2. Metadata is serialized to JSON, encrypted with `age.EncryptStream`, and written to `archives/<canonical_key>.meta.json.age` on all destinations.

---

## 6. Storage Provider Architecture

### 6.1 The Unified `storage.Provider` Interface

```go
type ObjectInfo struct {
    Name         string    // e.g. "archives/workstation/user/projects/rollmind.tar.zst.age"
    Size         int64
    Updated      time.Time
    StorageClass string
}

type Provider interface {
    Name() string
    Type() string // "gcs", "gdrive", "s3"
    NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error)
    NewReader(ctx context.Context, objectName string) (io.ReadCloser, error)
    List(ctx context.Context, prefix string) ([]ObjectInfo, error)
    Delete(ctx context.Context, objectName string) error
    Close() error
}
```

### 6.2 Google Cloud Storage (GCS) Provider
* **Library:** `cloud.google.com/go/storage`
* **Streaming:** Implements `bucket.Object(objectName).NewWriter(ctx)` returning an `*storage.Writer` directly compatible with `io.WriteCloser`.
* **Cost Optimizations:** Configured during `castor init` with automatic Object Lifecycle Management:
  * 30 days → Nearline Storage ($0.010/GB/mo)
  * 90 days → Coldline Storage ($0.004/GB/mo)
  * 365 days → Archive Storage ($0.0012/GB/mo, optional)

### 6.3 Google Drive Provider
* **Library:** `google.golang.org/api/drive/v3`
* **Root Hierarchy:** Operates inside a configurable root folder (default: `CastorLodge`).
* **Folder Hierarchy Auto-Resolution:** Recursively resolves path segments (e.g. `CastorLodge` → `workstation` → `user` → `projects`), creating missing folder nodes dynamically with `mimeType = "application/vnd.google-apps.folder"`. Resolved folder IDs are cached in memory.
* **Deduplication Handling:** Google Drive permits multiple files with identical names in the same folder. Castor queries the parent folder ID for existing files with matching names:
  * If file exists: calls `service.Files.Update(fileID, nil).Media(pr).Do()`.
  * If file is new: calls `service.Files.Create(meta).Media(pr).Do()`.
* **In-Memory Resumable Pipe Streaming:** Uses `io.Pipe()` with a background goroutine driving the Drive media upload, avoiding local disk staging.
* **Scope Security:** Requests exclusively `https://www.googleapis.com/auth/drive.file`. Castor can only read and write files it created; it cannot access personal Drive files.

### 6.4 Single-Pass MultiWriter Fan-Out
When multiple destinations are configured:
```
Target Reader -> tar.Writer -> zstd.Writer -> age.Writer -> MultiWriter
                                                              ├── GCS Object Writer
                                                              └── GDrive Pipe Writer
```
The target workspace is read, compressed, and encrypted exactly once. Bytes are broadcast concurrently to all active cloud destinations in real time.

---

## 7. Git Repositories: Working Tree + History Preservation

A critical failure mode of naive Git backup scripts is running only `git bundle create`. A Git bundle **only contains committed refs**; any uncommitted changes, staged index edits, or untracked files are lost if disaster strikes.

Castor provides complete, zero-data-loss Git preservation:

```
workstation/user/projects/rollmind.tar.zst.age
└── (internal tar layout)
    ├── .castor/
    │   └── repo.bundle              <- git bundle create --all refs/stash (all commits, branches, tags, AND stashes!)
    └── (working tree files)         <- modified, staged, and untracked files
        ├── src/
        │   └── index.ts
        └── package.json
        (filtered by [rules.git] excludes: node_modules/, target/, .git/, etc.)
```

### Restoration Workflow (`castor pull`)
1. Decrypts and unpacks the tarball to target directory.
2. Initializes Git repository: `git init`.
3. Restores refs from bundle: `git fetch .castor/repo.bundle 'refs/*:refs/*'`.
4. Checks out original branch and aligns HEAD.
5. If stash refs exist in bundle, restores stash: `git stash apply` or restores stash commit.
6. Removes temporary `.castor/` extraction artifact.
7. Result: 100% fidelity recovery of committed history, stashes, AND exact working tree state!

---

## 8. Configuration File Specification (`~/.config/castor/config.toml`)

```toml
# ==============================================================================
# Castor Master Configuration
# ==============================================================================

namespace = "workstation"   # Vault-scoped storage partition

[performance]
max_workers = 4             # 0 = auto (runtime.NumCPU())
compression_level = 19      # zstd level (1-22, 19 = optimal cold storage)

[safety]
max_archive_size_gb = 5.0   # Circuit breaker ceiling per target

[security]
encrypt = true
encryption_method = "age"
# Supports multiple public keys (e.g. workstation + offline recovery key)
age_public_keys = [
    "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
]
# age_private_key_file = "" # Kept offline by default

# ------------------------------------------------------------------------------
# Storage Destinations (Multi-Cloud Fan-Out)
# ------------------------------------------------------------------------------

[[destinations]]
name = "gcp-coldline"
provider = "gcs"
bucket = "castor-vault-montreal"
location = "northamerica-northeast1"
prefix = "archives"

[[destinations]]
name = "gdrive-mirror"
provider = "gdrive"
folder = "CastorLodge"

# ------------------------------------------------------------------------------
# Explicit Backup Targets
# ------------------------------------------------------------------------------

[[targets]]
path = "~/projects/rollmind"
type = "git"
compression = "zstd"
create_git_bundle = true

[[targets]]
path = "~/projects/work/client-a/api"
type = "git"
compression = "zstd"

[[targets]]
path = "~/Documents/Vault"
type = "documents"
compression = "xz"

[[targets]]
path = "/etc/caddy"
type = "documents"
compression = "zstd"

# ------------------------------------------------------------------------------
# Type-Aware Exclusion Rules
# ------------------------------------------------------------------------------

[rules.git]
excludes = [
    ".git", "node_modules", ".npm", ".yarn", ".pnpm-store",
    ".next", ".nuxt", ".turbo", "dist", "build", "out",
    "__pycache__", "*.pyc", ".venv", "venv", "env",
    "target", "bin", "obj", "*.log", ".DS_Store"
]

[rules.documents]
excludes = ["*.tmp", "temp", "tmp", "~$*", ".DS_Store", "Thumbs.db"]

[rules.media]
excludes = [".cache", "Thumbs.db", ".DS_Store"]

[rules.generic]
excludes = [".cache", "tmp", "temp", "*.tmp", ".DS_Store", "Thumbs.db"]
```

---

## 9. Operational Lifecycle & Automation

### 9.1 Systemd User Service & Timer
On Linux, Castor provides automated scheduled runs via systemd user units:

`~/.config/systemd/user/castor.service`:
```ini
[Unit]
Description=Castor Cold-Storage Vault Archiver
ConditionACPower=true
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/castor push
Nice=19
IOSchedulingClass=idle
```

`~/.config/systemd/user/castor.timer`:
```ini
[Unit]
Description=Run Castor Archiver on Schedule

[Timer]
OnCalendar=*-*-* 03:00:00
RandomizedDelaySec=1800
Persistent=true

[Install]
WantedBy=timers.target
```

### 9.2 Desktop Failure Notifications
When running as a systemd user service or cron job on Linux, Castor can send desktop alerts via `libnotify` (`notify-send`) if a backup run encounters an error:
```bash
notify-send -u critical -i dialog-warning "🦫 Castor Backup Alert" "Scheduled backup failed for target 'user/projects/rollmind'. Run 'castor status' for details."
```

---

## 10. Codebase Architecture (Go Project Structure)

```
castor/
├── cmd/
│   └── castor/
│       └── main.go                    # Entrypoint, root command dispatch
├── internal/
│   ├── cli/                           # Command definitions
│   │   ├── root.go                    # Base CLI flags & logging setup
│   │   ├── push.go                    # castor push implementation
│   │   ├── pull.go                    # castor pull & restore logic
│   │   ├── add.go                     # castor add directory scanner & TUI
│   │   ├── ls.go                      # castor ls remote listing (cross-namespace)
│   │   ├── status.go                  # castor status drift & timer check
│   │   ├── verify.go                  # castor verify archive health checker
│   │   ├── prune.go                   # castor prune orphan cleanup
│   │   ├── init.go                    # castor init onboarding wizard
│   │   ├── auth.go                    # castor auth login/logout/status
│   │   ├── doctor.go                  # castor doctor environment validator
│   │   └── upgrade.go                 # castor upgrade binary self-updater
│   ├── config/                        # Configuration loader & validator
│   │   ├── config.go                  # TOML unmarshaling, namespace, defaults
│   │   ├── keys.go                    # CanonicalCloudKey resolver with namespace scope
│   │   └── state.go                   # state.json load/save
│   ├── engine/                        # Core archiving & streaming engine
│   │   ├── archive.go                 # Tar packager, git working tree filter, Lstat handling
│   │   ├── fingerprint.go             # Microsecond Git & mtime fingerprinting
│   │   ├── metadata.go                # Sidecar .meta.json generation & parsing
│   │   └── pipeline.go                # Streaming pipeline orchestrator
│   ├── crypto/                        # Encryption layer
│   │   ├── age.go                     # Age multi-recipient X25519 keygen, EncryptStream, DecryptStream
│   │   └── hash.go                    # Stream hashing & verification
│   ├── storage/                       # Multi-cloud storage abstraction
│   │   ├── provider.go                # Provider interface & ObjectInfo
│   │   ├── multiwriter.go             # io.MultiWriter fan-out with error handling
│   │   ├── gcs.go                     # Google Cloud Storage provider
│   │   └── gdrive.go                  # Google Drive API v3 provider
│   ├── tui/                           # Rich Terminal UI components (Charm)
│   │   ├── theme.go                   # Beaver theme palette, borders, lipgloss styles
│   │   ├── form.go                    # Interactive Huh forms (init, auth, prompts)
│   │   ├── checklist.go               # Bubble Tea checklist with preview pane (add)
│   │   ├── picker.go                  # Bubble Tea fuzzy-search archive picker (pull)
│   │   ├── progress.go                # Live multi-worker progress dashboard (push)
│   │   └── table.go                   # Lipgloss-styled tables (ls, status, doctor)
│   └── sysinfo/                       # Host environment inspection
│       ├── host.go                    # Hostname, OS, architecture, user resolution
│       ├── notify.go                  # Desktop notifications via notify-send
│       └── systemd.go                 # Unit file generator & timer checker
├── go.mod
├── go.sum
└── Makefile
```

---

## 11. Critical Edge Cases & Production Considerations

### 11.1 Git Stashes & Untracked Files
* **Issue:** `git bundle create <file> --all` only captures branches and tags. It **omits `refs/stash`**!
* **Castor Solution:** Castor explicitly verifies if `refs/stash` exists (`git rev-parse --verify refs/stash`) and appends it to the bundle command:
  ```bash
  git bundle create .castor/repo.bundle --all refs/stash
  ```
  Combined with archiving the active working tree, zero uncommitted or stashed work is ever lost.

### 11.2 Shifting Files During Streaming (Live Workspaces)
* **Issue:** If a file is modified, truncated, or appended while Castor is traversing the filesystem, standard `tar.Writer` can fail with `ErrWriteTooLong` or unexpected EOF because file size no longer matches the header.
* **Castor Solution:** The file archiver wraps file reads with bounds checking:
  - If a file grew: reading stops at the recorded header size.
  - If a file shrank: remaining bytes are zero-padded to satisfy the header size, and a warning is logged (`[WARN] File %s modified during archive, padded to header size`).
  - The backup stream **does not fail** on transient file modifications.

### 11.3 Symlinks, Sockets & Permissions
* **Issue:** Naive walkers using `os.Stat` follow symlinks (risking loops or broken references) and can crash on Unix sockets (e.g. Docker sockets, dev server sockets).
* **Castor Solution:**
  - Archiver uses `os.Lstat` everywhere.
  - Sockets (`os.ModeSocket`), named pipes (`os.ModeNamedPipe`), and device nodes are skipped with a debug log.
  - Symlinks are preserved as `tar.TypeSymlink` storing the link target without following.
  - On restore, Castor enforces safe permissions (`0755` for directories, preserving execute bits, avoiding permission escalation).

### 11.4 Multi-Recipient Age Encryption
* **Issue:** If a user only encrypts with one key, losing that private key means all backups are permanently unrecoverable.
* **Castor Solution:** Castor supports multiple Age recipient public keys in `config.toml`:
  ```toml
  age_public_keys = [
      "age1ql3z...", # Workstation key
      "age1m7y2..."  # Offline paper / cold storage recovery key
  ]
  ```
  `age.Encrypt(w, recipients...)` encrypts the file header for all keys simultaneously with negligible overhead (<1 KB extra per key), enabling foolproof disaster recovery.

### 11.5 Zero-Disk Streaming & Network Drop Trade-Off
* **Reality:** In-memory zero-disk streaming (`tar -> zstd -> age -> cloud`) cannot seek backward if a TCP connection drops at 95% of an upload. Resuming would require a local disk buffer.
* **Castor Solution:**
  - For small-to-medium archives (<500 MB), failed streams retry from byte 0 with exponential backoff (up to 3 attempts).
  - Target size safety limits (`max_archive_size_gb`) prevent runaway stream sizes.
  - Per-destination state tracking ensures that if GCS succeeds but Google Drive drops, subsequent runs only re-stream to the incomplete destination.

### 11.6 Google Drive Personal Quota vs. Service Accounts
* **Issue:** Google Cloud Service Accounts do not share your personal Google One / Drive storage quota. They have an isolated, empty Drive with 0 quota.
* **Castor Solution:**
  - `castor init` explicitly uses **OAuth 2.0 loopback user consent** for Google Drive, attaching uploads directly to the user's personal Google One / Workspace account.
  - Google Drive REST API v3 rate limits (HTTP 429) are handled with jittered exponential backoff.

### 11.7 Archive Health Verification (`castor verify`)
* **Issue:** "Nobody wants a backup; everybody wants a restore." Backups must be verifiable without waiting for a disaster.
* **Castor Solution:** The `castor verify` command:
  - Fetches the encrypted archive stream and sidecar metadata from the cloud.
  - Decrypts on the fly in-memory.
  - Recomputes SHA-256 payload checksum against the sidecar manifest.
  - Tests tar/bundle header validity without unpacking files to disk.
  - Reports: `✔ workstation/user/projects/rollmind: Decrypted successfully · SHA-256 matched · 100% healthy`.

---

## 12. Conclusion

By combining **clean namespace partitioning**, **complete Git fidelity (including stashes)**, **Charm-powered interactive TUI ergonomics**, **multi-recipient Age encryption**, and **in-memory archive verification**, Castor delivers an exceptionally robust, elegant, and delightful cold-storage vault.

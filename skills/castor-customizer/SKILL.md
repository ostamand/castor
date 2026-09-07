---
name: castor-customizer
description: >-
  Customize, configure, extend, and adapt the Castor cold-storage archiver.
  Use this skill whenever modifying Castor's configuration (`config.toml`), adding or editing targets,
  configuring new storage backends (Google Cloud Storage, Google Drive, local filesystem / external drive / NAS),
  tuning compression levels (Zstandard / XZ) and worker concurrency, customizing exclusion rules and build junk filters,
  or implementing new custom storage providers and engine hooks in the Go codebase.
---

# Castor Customization & Extension Guide

This guide enables LLM agents and developers to safely modify Castor's configuration, tune performance, register custom targets, add storage backends, and extend the Go codebase.

--------------------------------------------------------------------------------

## Configuration Reference (`~/.config/castor/config.toml`)

Castor stores its declarative configuration in TOML. The default location is `~/.config/castor/config.toml`.

```toml
# Top-level namespace partition (e.g. "workstation", "laptop", "production", "homelab").
# Prevents cross-device collisions in shared buckets.
namespace = "workstation"

[performance]
max_workers = 4             # Concurrent upload workers (default: 4 or CPU cores / 2)
compression_level = 19      # zstd level (1-22, 19 = optimal for cold-storage archival)

[safety]
max_archive_size_gb = 5.0   # Circuit breaker ceiling per target (prevents accidental runaway backups)

[security]
encrypt = true
encryption_method = "age"
age_public_keys = [
    "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
]

# Destination 1: Google Cloud Storage (GCS)
[[destinations]]
name = "gcp-coldline"
provider = "gcs"
bucket = "my-castor-coldline"
location = "northamerica-northeast1"
prefix = "archives"

# Destination 2: Google Drive
[[destinations]]
name = "gdrive-personal"
provider = "gdrive"
folder = "CastorLodge"

# Destination 3: Dropbox (OAuth 2.0 PKCE, zero-disk streaming)
[[destinations]]
name = "dropbox-backup"
provider = "dropbox"
folder = ""                      # Empty string or omit to store directly in Apps/Castor Archiver/

# Destination 4: Local Filesystem / External NVMe / NAS Mount
[[destinations]]
name = "external-drive"
provider = "local"
path = "/mnt/backup/castor"

# Tip: You can also manage destinations directly via the CLI:
#   castor provider add dropbox --name dropbox-backup
#   castor provider add local /mnt/backup/castor --name external-drive
#   castor provider list
#   castor provider test
#   castor provider disable dropbox-backup  # Sets disabled = true (skipped by push & status)
#   castor provider enable dropbox-backup   # Re-enables destination
#   castor provider remove external-drive

# Target: Git Repository
[[targets]]
name = "castor"
path = "~/Work/git/castor"
type = "git"                     # "git", "documents", "media", "generic"
compression = "zstd"             # "zstd" or "xz"
create_git_bundle = true         # Bundles .git history, stashes, and tags into .castor/repo.bundle
# max_size_gb = 10.0             # Optional target-specific circuit breaker override
# destinations = ["gcp-coldline"]# Optional: route target strictly to specific destinations

# Target: Documents Folder
[[targets]]
name = "tax-records"
path = "~/Documents/Finance"
type = "documents"
compression = "xz"

# Exclusion Rules (glob patterns ignored during scanning and packaging)
[rules.git]
excludes = [
    ".git", "node_modules", ".npm", ".yarn", ".pnpm-store",
    ".next", ".nuxt", ".turbo", "dist", "build", "out",
    "__pycache__", "*.pyc", ".venv", "venv", "env",
    "target", "bin", "obj", "*.log", ".DS_Store"
]

[rules.documents]
excludes = ["*.tmp", "temp", "tmp", "~$*", ".DS_Store", "Thumbs.db"]

[rules.generic]
excludes = [".cache", "tmp", "temp", "*.tmp", ".DS_Store", "Thumbs.db"]
```

--------------------------------------------------------------------------------

## Programmatic Configuration Editing in Go

When building tools or scripts that edit Castor's configuration, **never use raw string concatenation or regex substitution** to avoid corrupting TOML array-of-tables (`[[targets]]`). Always use the typed Go API:

```go
import "github.com/ostamand/castor/internal/config"

// Load and validate
cfg, err := config.LoadConfig("/path/to/config.toml")
if err != nil {
    // handle error
}

// Add a target
cfg.Targets = append(cfg.Targets, config.TargetConfig{
    Name:            "new-service",
    Path:            "/home/user/code/service",
    Type:            "git",
    Compression:     "zstd",
    CreateGitBundle: true,
})

// Save atomically
err = config.SaveConfig("/path/to/config.toml", cfg)
```

--------------------------------------------------------------------------------

## Extending the Storage Engine

All storage backends implement the clean, zero-disk `storage.Provider` interface in [`internal/storage/provider.go`](file:///home/ostamand/Work/git/castor/internal/storage/provider.go):

```go
type Provider interface {
    Name() string
    Type() string
    NewWriter(ctx context.Context, objectName string) (io.WriteCloser, error)
    NewReader(ctx context.Context, objectName string) (io.ReadCloser, error)
    List(ctx context.Context, prefix string) ([]ObjectInfo, error)
    Delete(ctx context.Context, objectName string) error
    Close() error
}
```

### Steps to Implement a New Provider (e.g. AWS S3 or Cloudflare R2):
1. Create `internal/storage/s3.go` implementing `Provider`.
2. Register the provider in `NewProviderFromConfig` in `internal/storage/provider.go`:
   ```go
   case "s3", "r2":
       return NewS3Provider(ctx, dest.Name, dest.Bucket, dest.Prefix)
   ```
3. Add validation for required fields in `internal/config/config.go` (`Validate()`):
   ```go
   case "s3", "r2":
       if d.Bucket == "" {
           return fmt.Errorf("s3 destination '%s' requires a bucket", d.Name)
       }
   ```
4. Run regression tests with race detector:
   ```bash
   make test
   ```

--------------------------------------------------------------------------------

## Modifying the Interactive Charm TUI

Castor uses the Charm ecosystem for its terminal interface:
* **Interactive Forms & Wizards (`castor init`, overwrite prompts):** Implemented using `github.com/charmbracelet/huh` in [`internal/tui/form.go`](file:///home/ostamand/Work/git/castor/internal/tui/form.go).
* **Split-Pane Directory Checklist (`castor add`):** Implemented using `github.com/charmbracelet/bubbletea` in [`internal/tui/checklist.go`](file:///home/ostamand/Work/git/castor/internal/tui/checklist.go).
* **Fuzzy-Search Archive Explorer (`castor pull`):** Implemented in [`internal/tui/picker.go`](file:///home/ostamand/Work/git/castor/internal/tui/picker.go).
* **Live Streaming Progress Dashboard (`castor push`):** Implemented in [`internal/tui/progress.go`](file:///home/ostamand/Work/git/castor/internal/tui/progress.go).
* **Theme & Colors:** Defined in [`internal/tui/theme.go`](file:///home/ostamand/Work/git/castor/internal/tui/theme.go) using beaver warm wood & amber tones.

All TUI components automatically detect non-TTY environments (`!term.IsTerminal`) and fall back to clean text logging for cron, systemd, and CI execution.

--------------------------------------------------------------------------------

## Testing Code Changes

Always run the full suite before committing changes to ensure no regressions to core features:
```bash
# Run unit and integration tests with Go race detector
make test

# Recompile binary
make build
```

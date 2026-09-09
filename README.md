<div align="center">

<img src="assets/hero.png" alt="Castor - Sovereign Cold Storage for Developers" width="580" style="max-width: 100%; border-radius: 14px; box-shadow: 0 20px 60px rgba(255, 107, 0, 0.28), 0 8px 24px rgba(0, 242, 195, 0.15), 0 0 0 1px rgba(255, 107, 0, 0.25);">

# Castor 🦫

### Effortless, private backups to the storage you already own.

Back up all your projects, directories, and Git repositories in seconds directly to **Google Drive**, **Dropbox**, **GCS**, or your **home drive**.<br>
Encrypted by default, lightning-fast, and completely daemon-free.

<p align="center">
  <a href="https://github.com/ostamand/castor"><img src="https://img.shields.io/badge/release-v0.1.0-FF6B00" alt="Release"></a>
  <a href="https://github.com/ostamand/castor/actions/workflows/ci.yml"><img src="https://img.shields.io/badge/CI-passing-00F2C3" alt="CI"></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-FF8533" alt="License: MIT"></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.23+-06B6D4" alt="Go Version"></a>
</p>

```bash
curl -fsSL https://ostamand.com/castor/install.sh | bash
```

[Website](https://ostamand.com/castor) • [CLI Guide](./skills/castor-cli/SKILL.md) • [Omarchy Plugin](https://ostamand.com/castor/omarchy)

</div>

---

```
Your Projects & Repos ───(Encrypted In-Memory Stream)───┬───> Google Drive (Your idle storage)
                                                        ├───> Dropbox (Scoped App folder)
                                                        ├───> Google Cloud Storage (Cheap coldline)
                                                        └───> Local NAS / External Drive
```

---

## ⚡ The 30-Second Demo

### 1. Point at your projects
Castor auto-discovers Git repositories and automatically ignores disposable build junk (`node_modules`, `.venv`, `target/`, `build/`, `dist/`):
```bash
castor add ~/Work/git --scan --git-only
```

### 2. Plug in the storage you already own
No new subscriptions. Back up to your existing Google Drive, Dropbox, low-cost GCS, or home drive:
```bash
castor provider add dropbox                                       # 1-click browser OAuth into scoped App folder
castor provider add gdrive --folder CastorLodge                   # Google One / Drive
castor provider add gcs my-bucket --credentials ~/gcs-key.json    # Low-cost GCP Nearline/Coldline
castor provider add local /mnt/nas/castor --name local-nas        # Home NAS / external SSD
```

### 3. Stream directly into your vaults
Streams in-memory. Zero temporary tarballs filling up your SSD:
```bash
$ castor push
🦫 Castor · Fast Encrypted Backup Engine (workstation)

✔ [1/3] castor (git)      → google-drive, my-dropbox, local-nas [12.4 MB in 1.1s]
✔ [2/3] dotfiles (git)    → google-drive, my-dropbox, local-nas [1.2 MB in 0.2s]
✔ [3/3] notes (generic)   → google-drive, my-dropbox, local-nas [8.5 MB in 0.7s]

✔ Push complete! 3 targets synced, encrypted & streamed in 2.0s.
```

### 4. Inspect or pluck a single file in seconds
Need a `.env` or config file from a remote archive? Stream just that file without downloading the full archive:
```bash
castor cat my-app .env
```

### 5. CLI-Free Standalone Decryption (`restore.sh`)
Downloaded an archive manually from Dropbox or Google Drive onto a machine without the Castor CLI? Restore it with zero dependencies using our standalone script:
```bash
# Decrypt, decompress, and restore (including full git history):
./restore.sh my-app.tar.zst.age

# Or run in one line without cloning the repo:
curl -fsSL https://raw.githubusercontent.com/ostamand/castor/main/restore.sh | bash -s -- my-app.tar.zst.age

# Pluck a single file (.env) straight to stdout:
./restore.sh -f .env my-app.tar.zst.age > .env
```

---

## 💡 Core Principles: Why Castor?

Castor is engineered around simple, uncompromising principles:

### 💰 1. Use the Storage You Already Own
Why pay $10–$20/month for another cloud backup subscription when you already have gigabytes of idle storage? Castor connects directly to the providers you already use:
* **Dropbox:** Scoped App folder storage (`Apps/Castor Archiver/`) with seamless OAuth 2.0 PKCE login.
* **Google Drive:** Tap into the 2–5 TB included with your existing Google One plan.
* **Google Cloud Storage:** Archive tier at **$0.0012 / GB / mo** (< $0.15/year for 10 GB of cold storage).
* **Local NAS / External SSD:** Blazing-fast air-gapped snapshots at zero cost.

### 🔐 2. True Zero-Knowledge Encryption
Your code remains strictly yours:
* **Encrypted on Your Machine:** Every archive is encrypted client-side with modern Age (`age1...`) cryptography before touching the wire or remote storage.
* **The Cloud Never Sees Your Files:** Cloud providers only store encrypted blobs. File names, directory structures, and code contents are completely invisible.
* **Even Castor Has No Keys:** Castor operates purely client-side; only your public key is stored on your machine for backups. Castor never has access to your private key. Encryption keys stay under your sovereign custody.

### 🎯 3. Explicit Targets & Multi-Destination Fan-Out
* **Your Data Stays Where It Lives:** You never need to move repositories or drop files into a designated "Sync" folder. Castor reaches your projects wherever they are on your system.
* **You Choose What Is Saved:** Backups are explicitly declared in `config.toml`. Nothing is synced without your approval.
* **Stream Everywhere at Once:** Save to multiple places all at the same time—stream concurrently to Google Drive, Dropbox, GCS, and your local NAS in a single pass with failure isolation.

### 🧘 4. You're in Control Always
* **Zero Background Daemons:** No bloated desktop sync client running 24/7, eating 500 MB of RAM, thrashing CPU, indexing during a `git rebase`, or draining your laptop battery.
* **Explicit & Predictable:** Backups run only when you invoke `castor push`—or on lightweight, native system timers (`castor schedule enable`) that run while you sleep and immediately exit.

### 📦 5. 100% Git Fidelity & Clean Archives
* **Never Lose Work Again:** Backs up full commit history, local branch heads, uncommitted WIP edits, untracked scratch files, and active stashes (`refs/stash`) bundled into `.castor/repo.bundle`.
* **Developer-Aware by Default:** Automatically filters out disposable build junk (`node_modules`, `.venv`, `target/`, `dist/`), keeping your archives lean, fast, and focused on your actual code.

---

## ✨ Features You'll Love

* ⚡ **Peek Without Downloading (`castor cat`):** Grab a `.env` or single source file from cold storage in milliseconds straight to stdout without downloading full multi-gigabyte archives.
* 🚀 **Direct In-Memory Streaming:** Pipes encrypted archives straight to your cloud vaults in RAM. Zero temporary staging tarballs filling up your SSD.
* 🤖 **Built for AI Coding Agents:** Ships with native skills for Claude, Gemini, and Antigravity so autonomous coding assistants can safely verify, audit, and run backups for you.
* 🎨 **Delightful Terminal UI:** Snappy Lipgloss interface with live progress bars, target drift detection, and clear color-coded statuses.

---

## 🧰 Commands

Command | What It Does
:--- | :---
`castor push` | Packages, encrypts, and streams changed projects to your vaults (`-n` dry run, `-d` single destination)
`castor pull [target]` | Interactive fuzzy-search archive picker to restore any project
`castor status` | Shows target drift, backup freshness, and multi-destination sync health
`castor diff [target]` | Sub-second diff of local git commit/dirty state vs remote archive (zero download)
`castor cat <target> <file>` | Streams a single file from a remote encrypted archive directly to stdout or disk
`castor provider [list\|add\|rm\|disable\|enable\|test]` | Manage storage destinations (Local NAS, Google Drive, Dropbox, GCS)
`castor verify [target]` | In-memory stream decryption & SHA-256 integrity verification
`castor mv <target> <new-name>` | Renames a project and migrates remote cloud archives without re-uploading
`castor prune` | Safely cleans orphaned cloud archives no longer tracked in your config

---

## 🤖 Built for AI Agents & Customizers

Castor is engineered to be **agent-native**. It ships with pre-configured skills in `skills/` so autonomous AI coding agents (Claude, Gemini, Antigravity, Cursor) can manage your archives safely:

* **`castor-cli`**: Teaches agents how to onboard projects, inspect drift, and verify archives.
* **`castor-customizer`**: Guides agents on tuning Zstandard compression levels, editing filters, or extending the Go streaming engine.

```bash
make install-skills   # Symlinks skills to ~/.gemini/config/skills/
```

---

## 📚 Deep Dives & References

Need technical specifications or customization guides? Everything is documented in detail:

* 📖 **[CLI Operations Reference (`skills/castor-cli/SKILL.md`)](./skills/castor-cli/SKILL.md)** — Complete reference for every command, flag, and recovery procedure.
* 🛠️ **[Customization Guide (`skills/castor-customizer/SKILL.md`)](./skills/castor-customizer/SKILL.md)** — How to add new storage backends and tune compression.
* 🐧 **[Omarchy Linux Plugin](./internal/cli/status.go)** — System bar status indicators, idle triggers, and desktop notifications.

---

<div align="center">

**Castor** is 100% open-source software under the [MIT License](./LICENSE).<br>
Built with Go, Age, Zstandard, and Charm.

</div>

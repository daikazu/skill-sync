# skill-sync

`skill-sync` keeps your Claude Code skills, agents, commands, rules
(`CLAUDE.md`), and shareable settings in step across every machine you work
on. It wraps a private git repo you already control — you never touch git
directly — and syncs at the level of individual items (one skill, one agent,
one settings key) rather than raw files, so a 3-way merge can resolve most
changes automatically and only asks you about genuine conflicts. The same
engine builds `.skillpack` files: portable archives for backing up your whole
setup, migrating to a new machine, or handing a curated bundle to your team
without ever overwriting anything they've customized themselves.

## Install

```sh
brew install daikazu/tap/skill-sync
```

## Quickstart

1. Create a **private** GitHub repo to hold your synced Claude Code config
   (e.g. `claude-sync`). Nothing needs to be in it yet.
2. On your first machine, point `skill-sync` at it:

   ```sh
   skill-sync init git@github.com:you/claude-sync.git
   ```

   `init` clones the repo. If your local `~/.claude` is empty, everything
   from the remote is adopted as-is. If it already has content, `init` runs
   the normal classify/review flow (see below) to reconcile the two.
3. On every other machine, run the same `init` command, then:

   ```sh
   skill-sync sync
   ```

   whenever you want to push local changes and pull in changes made
   elsewhere.

## Commands

| Command | What it does |
|---|---|
| `skill-sync init <git-remote-url>` | Set up syncing on this machine against a private git remote |
| `skill-sync sync` | Sync this machine with the shared repo (fetch, classify, review conflicts if any, apply, push) |
| `skill-sync status` | Dry run: show what a sync would do, in both directions, without changing anything |
| `skill-sync log` | Show recent sync history from the repo's git log |
| `skill-sync rollback [snapshot-name]` | Restore `~/.claude` files from a pre-apply snapshot (defaults to the most recent; `--list` shows all, `-y` skips confirmation) |
| `skill-sync pack [--all] [--name] [--version] [-o file]` | Build a `.skillpack`: `--all` for a full backup of every shareable item, or an interactive picker for a curated team package |
| `skill-sync install <file.skillpack> [-y]` | Install or upgrade a package (never clobbers items it doesn't own) |
| `skill-sync uninstall <package-name>` | Remove a package's items — only those still owned and unmodified |
| `skill-sync packages` | List installed packages and flag any items you've since modified |
| `skill-sync config` | Show current sync configuration |
| `skill-sync config include-key <settings-key>` | Also sync this `settings.json` key |
| `skill-sync config exclude-key <settings-key>` | Stop syncing this `settings.json` key |
| `skill-sync config policy <item-id> <never-sync\|always-ask\|default>` | Set a per-item sync policy |

Every command accepts `--claude-dir` (default `~/.claude`) and `--sync-dir`
(default `~/.claude-sync`) if you need non-standard locations. Run
`skill-sync --version` to check your installed version, or
`skill-sync <command> --help` for a command's full flag list.

## How syncing works

Each sync pulls the shared repo, scans your local `~/.claude`, and hashes
everything into **items**: one per skill directory, agent file, command
file, `CLAUDE.md`, settings key, and the plugin-enabled list. Comparing
current hashes against what this machine last synced (never against the
other machine's state) classifies every item as in-sync, a one-sided push or
pull (including deletions), or a true conflict.

One-sided changes — including deletions made on another machine — apply
automatically; they're never silently dropped, remote-originated deletions
are always called out explicitly in the sync summary. Only genuine
conflicts (the same item changed differently on two machines, or created
independently on both) open a review screen, where you choose per item:
**keep local**, **keep remote**, or **skip** (ask again next time). If
nothing conflicts, `sync` finishes with a short plain-text summary and no
review screen at all.

Settings sync key-by-key, not as one file: changing `model` on one machine
and `permissions` on another never conflicts with each other. Only a
built-in list of shareable, non-device-specific keys sync at all (things
like `model` and `permissions` — not `env`, `statusLine`, or anything
carrying an absolute path or device identity); adjust the list with
`skill-sync config include-key` / `config exclude-key`. Enabled plugins and
marketplaces sync separately, merged at the entry level, so enabling
different plugins on different machines just adds both.

Every apply is preceded by a snapshot of the files it's about to touch,
written to `~/.claude/backups/skill-sync/<timestamp>/`. If anything looks
wrong afterward, `skill-sync rollback` restores the most recent snapshot (or
pass a specific timestamp; `--list` shows what's available).

## Packages

A package is a single `.skillpack` file (a `.tar.gz` of selected items plus
a manifest) that never overwrites content it doesn't own:

- **Personal backup** — `skill-sync pack --all -o backup.skillpack` grabs
  your entire shareable profile into one file. Fully offline, no git remote
  needed; restore anywhere with `skill-sync install backup.skillpack`.
- **Migration** — installing a package on an empty machine adopts
  everything; on a machine that already has content, it runs the same
  classify/review flow as `sync`.
- **Team distribution** — `skill-sync pack` (without `--all`) opens a picker
  to check off exactly which items go in the bundle. Settings and rules are
  excluded by default for curated packs; opt them in per item.

On install, every item is recorded in an ownership ledger, and the tool
**never overwrites an item it doesn't own**: a name collision with something
you already have offers install-renamed, skip, or an explicit (snapshotted,
reversible) replace. Upgrading a package only touches items it still owns —
if you've since edited one yourself, that shows up as a conflict instead of
being silently reverted. `skill-sync uninstall` removes only the
ledger-owned items you haven't touched. Package-owned items are excluded
from your own personal sync by default, since team distribution and personal
sync are separate lanes; you can opt a specific item into personal sync if
you want to keep evolving it yourself. `skill-sync packages` lists what's
installed and flags anything you've modified since.

## Releasing

Releases are built by [GoReleaser](https://goreleaser.com) via
`.github/workflows/release.yml` whenever a `v*` tag is pushed: it builds
darwin/linux × amd64/arm64 binaries and pushes an updated formula to
`daikazu/homebrew-tap`. This requires a repo secret named
`TAP_GITHUB_TOKEN` — a GitHub PAT with write access to the
`daikazu/homebrew-tap` repository — in addition to the automatically
provided `GITHUB_TOKEN`.

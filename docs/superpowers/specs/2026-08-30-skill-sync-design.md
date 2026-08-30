# skill-sync — Design Spec

**Date:** 2026-08-30
**Status:** Approved (brainstormed and confirmed section-by-section with Mike)

## Purpose

A CLI + TUI tool that syncs Claude Code skills, agents, commands, rules, and
non-device-specific settings between a user's machines, with item-level 3-way
merge and interactive conflict resolution. The same machinery produces
**packages**: portable archives that serve as backups, migration bundles, and
curated team distributions that never clobber a recipient's own content.

## Decisions (from brainstorming)

| Decision | Choice |
|---|---|
| Form | CLI + full-screen TUI (Bubble Tea) |
| Language | Go |
| Sync backbone | Private git repo (tool wraps git; user never touches it) |
| Sync semantics | Item-level 3-way merge; ask only on true conflicts |
| Team distribution | Tracked install via the tool + ownership ledger (NOT packages-as-plugins — avoids "plugin inception" since the plugin list itself is synced) |
| Distribution of tool | GoReleaser → GitHub releases → `daikazu/homebrew-tap` (`brew install daikazu/tap/skill-sync`) |

Rejected alternatives: git-native line-level merging (item-blind, ugly
conflicts on settings.json); CRDT/server-backed sync (overkill); packages
delivered as Claude Code plugins (inception with synced plugin list).

## Core model

### Items

Everything the tool touches is an **item** — the atomic unit of syncing,
diffing, conflict resolution, and packaging.

| Item type | Source | Unit |
|---|---|---|
| Skill | `~/.claude/skills/<name>/` | whole directory (tree hash) |
| Agent | `~/.claude/agents/<name>.md` | file |
| Command | `~/.claude/commands/<name>.md` | file |
| Rules | `~/.claude/CLAUDE.md` | file |
| Setting | one key in `~/.claude/settings.json` | key + JSON value |
| Plugins | `enabledPlugins` + `extraKnownMarketplaces` keys | set of entries |

Item IDs are stable strings: `skill/humanizer`, `agent/php-pro`,
`command/security-review`, `rules/CLAUDE.md`, `setting/model`,
`plugins/enabledPlugins`, `plugins/extraKnownMarketplaces`.

Settings are split by a built-in **shareable-keys list** (default shareable:
`model`, `effortLevel`, `permissions`, and similar behavior-shaping keys;
default excluded: `env`, `statusLine`, `tui`, `voice*`, anything containing
absolute paths or device identity). `enabledPlugins` and
`extraKnownMarketplaces` are NOT settings items — they are handled exclusively
by the Plugins item type, which merges at the entry level. The user can override per key in tool config. Each key is
its own item, so changing `model` on one machine and `permissions` on another
never conflicts.

The plugins item merges as a set at the entry level (a plugin enabled on one
machine and a different plugin enabled on another → both survive; only
conflicting changes to the *same entry* conflict).

### State (three pieces)

1. **Sync repo** — local clone at `~/.claude-sync/repo/` of the user's
   private git remote. Holds canonical items in a mirrored layout
   (`skills/`, `agents/`, `commands/`, `rules/`, `settings.json` containing
   only shareable keys, `plugins.json` holding the plugins item's entries)
   plus a repo `manifest.json` (schema version, item index with hashes).
   Every sync commits → history/rollback for free.
2. **Device state** — `~/.claude-sync/state.json`, local only, never in git.
   Records last-synced hash per item for this machine. This is the "base" of
   the 3-way diff.
3. **Ownership ledger** — `~/.claude-sync/ledger.json`, local only. Records
   `item → {package, version, installed hash}` for package-installed items.

Tool config: `~/.claude-sync/config.json` (per-device: git remote, settings
key overrides, per-item sync policies `never-sync` / `always-ask`).

### Hashing

Content hash per item: SHA-256. Files hash their bytes; skills hash a
deterministic tree (sorted relative paths + per-file hashes); settings items
hash canonical JSON of the value; the plugins item hashes the sorted entry
set. Hash comparison against device state classifies every item.

## Sync flow

`skill-sync sync` stages:

1. **Fetch** — pull sync repo. Devices only push after successful sync, so
   history stays effectively linear. Push rejection later → re-fetch and
   re-classify automatically; never force-push.
2. **Scan** — inventory local `~/.claude` into items; hash; load device state.
3. **Classify** — per item: `in-sync`, `push`, `pull`, `conflict`,
   `new-local`, `new-remote`, `deleted-local`, `deleted-remote`,
   `conflict-both-new` (same name created independently both sides),
   `conflict-delete-modify`.
4. **Plan** — one-sided changes (including deletions) auto-apply; conflicts
   queue for review. Items owned by a package are excluded from sync by
   default (see Packages). Items with `never-sync` policy are skipped;
   `always-ask` items always queue.
5. **Review (TUI)** — opens **only if there is something to decide**.
   Left pane: conflicted items with type badges. Right pane: diff (rendered
   key-level diff for settings; unified file diff otherwise). Choices per
   item: **keep local** / **keep remote** / **skip** (ask again next sync).
   Footer summarizes auto-applying changes; any auto item can be demoted to
   skip. Remote-originated deletions are always listed explicitly in the
   summary — never buried.
6. **Apply** — snapshot affected local files to
   `~/.claude/backups/skill-sync/<timestamp>/`; write local changes; write
   remote-bound changes into sync repo; commit with readable message
   (`<hostname>: update skill humanizer, set model`); push; update device
   state.

Quiet sync (no conflicts): short plain-text summary, no TUI.

Other commands on the same engine:

- `skill-sync status` — dry-run classification, both directions.
- `skill-sync log` — recent sync history from git log.
- `skill-sync rollback [timestamp]` — restore from a pre-apply snapshot
  (defaults to the most recent one; snapshots are listed when ambiguous).
- `skill-sync init <git-remote-url>` — clone repo; empty local `~/.claude` →
  adopt everything from remote; populated local → normal classify/review.
  Migration handled by the same code path.

## Packages

A **package** is a `.skillpack` file: `.tar.gz` containing selected items in
the mirrored layout plus `manifest.json` (name, version, author, created-at,
item list with hashes, optional per-item description).

Three uses, one format:

1. **Backup** — `skill-sync pack --all -o backup.skillpack`: entire shareable
   profile. Fully offline; no git remote needed.
2. **Migration** — `skill-sync install backup.skillpack` on a fresh machine
   adopts everything; on a populated machine runs classify/review first.
3. **Team distribution** — `skill-sync pack` (no `--all`) opens a TUI picker
   to check off items. Settings and rules default to **excluded** for curated
   packs (opt-in per item). Output: `name-version.skillpack`.

### No-clobber rules on install

- Each installed item is recorded in the ownership ledger.
- **The tool never overwrites an item it doesn't own.** Name collision with
  the recipient's own item → TUI offers: install renamed
  (`code-review-agency`), skip, or explicit replace (snapshotted, reversible).
- Upgrading a package touches only ledger-owned items; a package-owned item
  the dev has locally edited (hash mismatch vs. ledger) surfaces as a
  conflict, never silently reverted.
- `skill-sync uninstall <name>` removes only ledger-owned, unmodified items.
- Package-owned items are **excluded from the recipient's own sync** by
  default (personal sync and team packages are separate lanes); overridable
  per item to "adopt" a team item as personal.
- `skill-sync packages` lists installed packages and flags modified items.

## CLI surface

```
skill-sync init <git-remote-url>
skill-sync sync
skill-sync status
skill-sync log
skill-sync rollback [timestamp]
skill-sync pack [--all] [-o file]
skill-sync install <file>
skill-sync uninstall <name>
skill-sync packages
skill-sync config
```

## Error handling

- Every apply preceded by snapshot; mid-apply failure prints the rollback
  command.
- Push rejection → automatic re-fetch + re-classify; never force-push.
- Unparseable settings.json or malformed skill dir → item flagged and
  skipped; sync continues for everything else.
- No network → clean failure at fetch, nothing touched. `pack`/`install`
  fully offline.
- Tarball hygiene: reject `.skillpack` entries with path traversal (`../`),
  absolute paths, or symlinks escaping the extract root.

## Testing

- Sync engine (scan/hash/classify/plan) is pure logic over a filesystem
  abstraction — table-driven unit tests for every classification case,
  including both-sides-new, delete-vs-modify, and locally-edited package
  items.
- Integration tests: real binary against temp dirs with a local bare git
  repo as "remote," simulating two devices syncing.
- TUI: Bubble Tea model unit tests (feed messages, assert state). No
  screenshot testing.

## Release

- GoReleaser on tagged pushes via GitHub Actions: darwin/linux, arm64+amd64.
- Formula pushed to `daikazu/homebrew-tap`; install via
  `brew install daikazu/tap/skill-sync`.

## Out of scope (YAGNI)

- Background daemon / file watching — sync is manual.
- Team sync server — packages travel as files.
- Windows support (can come later; nothing precludes it).
- Syncing plugin *code* — Claude Code manages plugin installation itself;
  only the enabled list + marketplaces sync.
- Project-level `.claude/` directories — global `~/.claude` only, v1.

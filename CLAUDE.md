# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

orgzd is a Go HTTP server + CLI that reads Emacs org-mode files (default `~/todo`, configurable) and presents them as a web agenda similar to the Android Orgzly app. The org files are synced between devices via Syncthing; orgzd is one of the consumers.

## Commands

- Build: `go build ./cmd/orgzd/`
- Run web server: `./orgzd` (defaults: dir `$HOME/todo`, listen `:8042`)
- Run with options: `./orgzd -dir /path -listen 127.0.0.1:9000`
- Print agenda to terminal: `./orgzd -cli`
- Tests: `go test ./...` (currently covers the sync-conflict merge in `internal/org/conflict_test.go`); other behaviour is tested manually against a copy of `~/todo` in `/tmp/orgzd-test`.

## Architecture

Three packages under `internal/`, plus a thin entry point in `cmd/orgzd/main.go`.

**`internal/org`** — org-mode parser and file mutators. The parser (`org.go`) is line-oriented with a small state machine: `inPlanning` (right after a headline, looking for SCHEDULED/DEADLINE/CLOSED), `inDrawer` (inside `:PROPERTIES:`/`:LOGBOOK:`/etc — content skipped), body collection (everything else, until the next headline). `done.go` implements the "mark done" file mutation, including repeater advancement for recurring tasks (`+1w`, `++1y`, `.+1m`). `archive.go` implements Emacs-style archive (move DONE entries to `archive.org` with `ARCHIVE_TIME`/`ARCHIVE_FILE`/`ARCHIVE_CATEGORY`/`ARCHIVE_TODO` properties). `conflict.go` merges Syncthing `*.sync-conflict-*.org` copies back into their base file (see below).

**`internal/agenda`** — pure function that takes parsed `[]*org.Entry` and a `now` time and produces grouped buckets (Overdue / Today / Tomorrow / This week / Later). No I/O. The agenda `Entry` type embeds `*org.Entry` so template code can reach every field directly.

**`internal/server`** — HTTP routes using Go 1.22+ method-based routing (`GET /`, `GET /maintenance`, `POST /api/done`, `POST /api/archive`). HTML templates are `embed`-ed (`agenda.html`, `maintenance.html`). The web UI uses `<details>/<summary>` for inline expansion of entry body — no SPA framework.

## File-mutation invariants (important)

All edits to org files happen via the `org` package and must preserve user data. When modifying these functions, keep in mind:

- **Mark done on a repeater** keeps the state and advances SCHEDULED by the repeater (`+`/`++` from original date until ≥ today; `.+` from today). It also updates `:LAST_REPEAT:` inside the existing `:PROPERTIES:` drawer, creating one if absent.
- **Mark done on a non-repeater** changes the headline state to the file's first done state (default `DONE`) and prepends `CLOSED: [timestamp]` to the existing planning line (or inserts a new line if there is no planning line).
- **Archive** processes per-file in **descending line order** so removals don't invalidate the line numbers of remaining targets. It writes the new content of source files in-place and appends the moved sub-trees to `archive.org`. The archive write is the last step so a mid-operation failure cannot lose data (worst case: an entry exists in both source and archive, which is harmless).
- Path validation: API handlers reject `file` fields containing `/` or `..` before passing to the org package.

## Sync-conflict merge (`internal/org/conflict.go`)

Syncthing renames the losing copy of a concurrently-edited file to `name.sync-conflict-YYYYMMDD-HHMMSS-DEVICE.org` and keeps the winner under the base name. These copies are excluded from `ParseDir`/`ListFiles`/`FindArchivable` so they never show up as duplicate agenda entries. On every agenda load the server tries to merge them away (`AutoMergeConflicts`, serialized by a mutex):

- Merging is entry-level: files split into level-1 subtrees keyed by title+tags (state keyword stripped, duplicate titles get `#n` suffixes). Untouched blocks keep their exact source bytes — merge must not reformat the file.
- **Union**: entries present in only one copy are kept, inserted after the nearest preceding shared entry.
- **Auto-resolve** (when bodies match ignoring the planning line and `:LAST_REPEAT:`): a done version beats a not-done one (completion must never be lost), otherwise the base file wins (it's Syncthing's winner).
- **Manual**: entries whose body text differs are shown on `/conflicts` (linked from a banner on the agenda) where the user picks a version per entry, or discards the conflict file wholesale.
- Before any write/delete both originals are backed up to `~/.cache/orgzd/conflict-backups/<timestamp>/`. The base file is written before the conflict copy is removed, so a mid-operation failure only means the merge re-runs.

## Org-mode quirks the parser handles

- Per-file `#+TODO: TODO WAIT | DONE CANCELED` directives override the default state keywords. `payments.org` has no such line and falls back to defaults.
- Headlines may have no state at all (just `* Title`). These still appear in the agenda if scheduled.
- Tags at end of headline: `:tag1:tag2:` — multiple tags separated by colons.
- Day name in timestamps is optional: both `<2026-05-21 Thu 09:50>` and `<2026-08-01 11:00>` parse.
- Timestamps parse in local time (`time.ParseInLocation`), not UTC — otherwise date-bucketing breaks around midnight.
- `archive.org` is excluded from `FindArchivable` so it can't be cannibalized.

## User-facing behaviours worth knowing

- Web page auto-refreshes every 60 s (`<meta http-equiv="refresh">`).
- **Inbox block**: not-done entries of `inbox.org` that have no SCHEDULED/DEADLINE are shown in a collapsed `<details>` block below the agenda groups (hidden when empty). Quick buttons schedule a single entry or the whole inbox to today/tomorrow via `POST /api/schedule`; once dated, an entry graduates to the normal date buckets (it stays in `inbox.org`). `RescheduleAll` inserts a SCHEDULED line when the entry has none, processing lines in descending order per file so insertions don't shift pending line numbers.
- The done checkbox is a styled `<button>`, not an `<input type="checkbox">`, because clicks inside a `<summary>` natively toggle the `<details>` — using a button lets us fully control the event.
- Maintenance page UX is "exclude, not include": every DONE entry starts checked, since the typical operation archives nearly everything; the user unchecks the few to keep. With ~700 entries this matters.

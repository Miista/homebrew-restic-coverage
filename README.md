# restic-coverage

Answers a question restic can't: **is there anything on disk that nobody ever
decided about?** Backup tools faithfully save what you configured — this
audits everything you *didn't* configure. Every file must be accounted for:
tracked in git (config, covered by the repo + its remote), backed up by
restic, excluded by the profile, or listed in an ignore file with a written
reason. Anything else fails the audit.

Built for [resticprofile](https://github.com/creativeprojects/resticprofile)
running in a Docker container.

## Install

```sh
# Homebrew (macOS/Linux)
brew install Miista/restic-coverage/restic-coverage

# Debian/Ubuntu — one-time repo setup (signed Cloudsmith apt repo)
sudo install -d /usr/share/keyrings
curl -1sLf https://dl.cloudsmith.io/public/guldmund/stable/gpg.key \
  | sudo gpg --dearmor -o /usr/share/keyrings/guldmund-stable-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/guldmund-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/guldmund/stable/deb/debian any-version main" \
  | sudo tee /etc/apt/sources.list.d/guldmund-stable.list
sudo apt update && sudo apt install restic-coverage
```

## How it works

1. Derives the **exact** restic command resticprofile would run
   (`resticprofile --dry-run`), so sources and excludes are never
   reimplemented or guessed.
2. Runs it with `--dry-run --verbose=2` to get the true would-be-included
   file list from restic itself.
3. Builds the **candidate set** — the files that must be accounted for.
   Two baselines (`--mode`, default `auto`):
   - **git** (when the tree is a git repo): every file *not* in the git
     index. Complete by construction — tracked files are config, covered by
     the repo and its remote; everything else needs another decision. The
     index is read directly from `.git/index`, no git binary required.
   - **data** (fallback for trees not under git): the heuristic scan —
     anything under a `*/data/` dir, plus `*.db`, `*.sqlite*`,
     `credentials*`, `*.key`, `*.pem`.
4. Diffs the candidates against the would-be-included set, then filters the
   remainder through the profile's `--exclude` patterns and the
   `coverage-ignore` file.
5. Whatever is left is new, undecided data → nonzero exit, optional alert.

In git mode the audit also *advises* (without failing) when a git-tracked
file looks like data or a secret — committing such a file quietly exempts it
from the audit, so it's worth a deliberate second look.

> **Split/sparse/v4 indexes** are rejected rather than misread — a wrong
> tracked set would silently corrupt the candidate set. Use `--mode=data`
> to audit such a tree with the heuristic instead.

The binary runs in two modes, detected automatically:

- **host mode** — resticprofile lives in a container; the tool shells into
  it (`docker exec`) for the dry-run and scans the tree on the host.
- **container mode** — resticprofile is on PATH (the tool runs *inside* the
  container, e.g. from cron); the dry-run runs directly and the tree is
  scanned through its in-container mount (`--scan-root`).

## Usage

```sh
restic-coverage                          # run the audit
restic-coverage ignore PATTERN REASON    # documented exception + re-audit
restic-coverage version
```

`PATTERN` is a shell glob matched against full host paths, also tried
anchored at any depth (`foo/data/*` matches `/srv/x/foo/data/y`). The
reason is mandatory: the ignore file is the documented record of every
deliberate omission.

## Configuration

Flags, with environment fallbacks:

| Flag | Env | Meaning | Default |
|---|---|---|---|
| `--container` | `RESTIC_CONTAINER` | resticprofile container | `restic`, else autodetect by image |
| `--profile` | `COVERAGE_PROFILE` | profile holding the backup section | `default` |
| `--scan-root` | `COVERAGE_SCAN_ROOT` | in-container mount of the audited tree | `/hostenv` |
| `--host-root` | `COVERAGE_HOST_ROOT` | host path the tree corresponds to | derived from the first `/home/*/docker` source |
| `--mode` | `COVERAGE_MODE` | candidate set: `git`, `data`, or `auto` | `auto` (git when the tree has a `.git`) |
| `--ignore-file` | `COVERAGE_IGNORE_FILE` | exception file | `<host-root>/<box>/restic/coverage-ignore` (host mode), `/resticprofile/coverage-ignore` (container mode) |
| `--notify` | `COVERAGE_NOTIFY=on` | push result to the notify hooks | off |

## Scheduled runs

The static binary runs fine inside alpine-based resticprofile containers.
Bind-mount it and symlink it into crond's periodic dirs at container start
(don't bind single files you intend to update — they pin the old inode):

```yaml
command:
  - >-
    resticprofile schedule --all &&
    ln -sf /usr/local/bin/restic-coverage-weekly /etc/periodic/weekly/restic-coverage &&
    crond -f
volumes:
  - /usr/bin/restic-coverage:/usr/local/bin/restic-coverage:ro
```

where `/usr/local/bin/restic-coverage-weekly` is a two-line wrapper
(`run-parts` passes no arguments):

```sh
#!/bin/sh
exec /usr/local/bin/restic-coverage --notify check
```

With `--notify`, the result goes to `/shared/notify-status.sh coverage
<true|false>` (heartbeat, e.g. Gatus) and failures additionally to
`/shared/notify-failure.sh coverage` (alert, e.g. ntfy) — if those scripts
exist. Adapt or omit them for your own alerting.

## The ignore file

One glob per line, `# reason` required:

```
*/pihole/data/*   # toml subdir IS backed up; gravity/lists regenerable
*/timemachine/*   # Time Machine target — is itself a backup medium
```

Keep deliberate omissions here rather than as profile excludes when they
overlap a parent of something backed up: restic excludes are live (and have
no negation), so `caddy/data/*` as an exclude would silently drop a
`caddy/data/certs` source. The ignore file is only ever read by the audit.

## Development

```sh
go test ./... -race -cover
```

Releases are tagged (`vX.Y.Z`); GoReleaser publishes the GitHub release,
the Homebrew formula (this repo doubles as the tap), and a `.deb` that the
release workflow pushes to the shared [Cloudsmith](https://cloudsmith.io) apt
repo (`guldmund/stable`), which indexes and signs it server-side.

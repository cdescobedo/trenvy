# Contributing to trenvy

Thanks for looking. This is a small, opinionated tool with one maintainer, so
this document is mostly about what a change needs to look like to be
mergeable — the conventions here are stricter than the size of the project
suggests, and they are the point.

## Getting set up

You need Go 1.27 or newer. That is the whole toolchain; there is no Makefile,
no code generation, and no build tags to remember.

trenvy is not published yet — there is no remote to clone from. Work from
your existing copy of the source:

```bash
cd trenvy
go build ./...
go test ./...
```

To run your build without installing it over a real one:

```bash
go build -o /tmp/trenvy ./cmd/trenvy
/tmp/trenvy --help
```

### Try it without touching your real setup

`TRENVY_CONFIG_DIR` overrides the config directory completely. Point it at a
scratch directory and trenvy will read and write nothing of yours:

```bash
export TRENVY_CONFIG_DIR=$(mktemp -d)
export TRENVY_BACKEND=file
export TRENVY_PASSPHRASE=scratch-only
```

Note the two extra variables. `TRENVY_CONFIG_DIR` sandboxes the config
directory, **not the keyring** — without `TRENVY_BACKEND=file`, a `secret set`
in your scratch project writes a real entry into your real login keychain and
you have to clean it up by hand. The file backend keeps everything, secrets
included, inside the scratch directory.

## The checks

Before you open a pull request:

```bash
go build ./...
go vet ./...
go test ./... -count=1
gofmt -l .        # must print nothing
```

CI runs `go vet` and `go test ./... -race` on macOS, Linux and Windows, and
separately cross-compiles for darwin/arm64, darwin/amd64, linux/amd64,
linux/arm64 and windows/amd64 with `CGO_ENABLED=0`.

That last job is not redundant. Every secret backend was deliberately chosen to
be cgo-free — the macOS one drives `security(1)` as a subprocess rather than
linking Security.framework — so trenvy cross-compiles for any target from any
machine. **If you add a dependency that needs cgo, that job is what will catch
it, and the answer is almost always to find another way.**

### The test suite is hermetic, and must stay that way

Tests point `TRENVY_CONFIG_DIR` at `t.TempDir()` and use the in-memory secret
store, so the full suite touches no real config directory and no real keyring.
Run it on any machine, in any order, in parallel, with no setup.

Keep it that way. A new test must not read `~/.config/trenvy`, must not write
to a real keyring, and must not depend on the machine having an unlocked login
keychain — CI runners do not.

The one exception is the macOS Keychain integration test, which is opt-in:

```bash
TRENVY_INTEGRATION=1 go test ./internal/store/
```

It is skipped by default and is not part of CI. If you touch
`internal/store/darwin.go`, run it locally on a Mac and say so in the pull
request.

`docs/acceptance.md` is a separate, manual, end-to-end walkthrough against real
repositories and a real keyring. It is deliberately not automated. It is worth
running when you change the render guards or the profile resolution, and not
otherwise.

## What a good change looks like here

### Comments explain why, not what

This is the convention the codebase cares about most. The code says what it
does; a comment earns its place by saying why it is that way — what breaks if
you do the obvious thing instead.

Compare. This adds nothing:

```go
// Sort the names.
sort.Strings(names)
```

This is the kind that belongs:

```go
// sortedVarNames is the iteration order both resolvers use. Go randomises map
// iteration, so without sorting here a rendered file's byte order -- and so
// its recorded file digest -- would differ run to run.
```

The second tells a future reader why they must not remove it.

**A comment that contradicts its code is worse than no comment**, because a
reader will trust it over the code. If you change behaviour, re-read every
comment near the change and fix the ones that are now lying. This has been the
single most common review finding on this project.

Prefer a comment on the declaration, phrased as a sentence starting with the
name being declared, per normal Go style.

### Error messages name the fix

An error here is expected to tell the user what to do next, not just what went
wrong. The codebase does this consistently and a change that regresses it will
be sent back:

```
API_TOKEN is not in the keyring for profile "dev"; run: trenvy -p dev secret set API_TOKEN
```

```
tracked.env is not ignored by git
  add to .gitignore:
    tracked.env
  or re-run with --allow-tracked
```

Two rules that fall out of this:

- **Never put a secret value in an error, a log line, or a success message.**
  Report the variable's name, or its length. `secret set` prints
  `stored API_TOKEN (16 bytes)` for exactly this reason.
- **Say what already happened.** When a multi-step operation fails partway —
  deleting a variable's secret across several profiles, say — the error says
  how many were already deleted and what survived. Implying nothing happened is
  how the rest never gets cleaned up.

### Tests pin behaviour, including the wording

Table-driven tests, `t.TempDir()`, no global state. The suite deliberately
asserts on user-visible error text, because the text is part of the contract —
if you improve a message, update the test that pins it in the same commit.

New behaviour needs a test. A bug fix needs a test that fails before it.

### Keep the two axes separate

The central design idea is that a **profile** decides what a value is and an
**output** decides where it goes, in what format, and which subset. They are
independent, and the manifest has no per-output value table on purpose. A
change that lets an output carry values, or a profile carry a path, is
reworking the data model rather than adding a feature — open an issue first.
`docs/architecture.md` explains the shape.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/). Types in use:
`feat`, `fix`, `test`, `refactor`, `docs`. Scopes are package names — `cli`,
`config`, `manifest`, `resolve`, `store`, `guard`, `cmd/trenvy` — and are
optional when a change is not package-specific.

```
fix(cli): tell the truth when edit's keyring writes fail partway through
```

Append `!` for a breaking change: `refactor!: rename the tool from envctl to trenvy`.

There is no release automation in this repository — nothing parses these to cut
a version. They are for the humans reading `git log`, which is why the bodies
here tend to be long: say why the change was made and what you rejected, not
what the diff already shows.

## Pull requests

- One logical change per pull request.
- Say how you verified it. "Ran the quickstart against a built binary" beats
  "should work".
- If you found a bug you are not fixing, say so rather than fixing it in
  passing — a reviewable diff does one thing.
- Update `CHANGELOG.md` under `## [Unreleased]` for anything user-visible.

## Reporting bugs

Include your OS, the backend (`keychain`, `system`, `file`), the commit you
built, and the exact command and output. **Redact secret values** — and if a
real one leaked into output you are about to paste, rotate it first.

For anything with a security impact, do not open a public issue. See
[SECURITY.md](SECURITY.md).

## Licence

By contributing you agree that your contributions are licensed under the
Apache License 2.0, the same terms as the rest of the project. See
[LICENSE](LICENSE). There are no per-file copyright headers in this repository
and none should be added.

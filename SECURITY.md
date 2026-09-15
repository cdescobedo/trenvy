# Security policy

trenvy holds credentials, so it is worth being precise about what it does and
does not protect. This document is the threat model and the reporting process.
The README has a shorter summary aimed at users.

## Supported versions

**None yet.** There are no tagged releases, no published binaries, and no
maintenance branches. The only supported version is the current `main`.

If you are running trenvy, you built it from a commit. Report against that
commit, and include it in your report.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

**trenvy is not published.** There is no repository to file against: no
remote, no tags, no releases, no advisories.

Report by email to `cescobedo@complecti.io`, subject line `trenvy security`.

When the project is published at `github.com/cdescobedo/trenvy`, reporting
will move to GitHub's private vulnerability reporting — the **Security** tab,
then **Report a vulnerability** — and email will remain a fallback.

Please include:

- the commit you are running,
- your OS and the secret backend in use (`keychain`, `system`, `file`),
- what an attacker gains, and what access they need to start with,
- a reproduction, ideally against a scratch `TRENVY_CONFIG_DIR`.

This is a personal project with one maintainer and no paid support, so there is
no response-time guarantee. Expect acknowledgement rather than a fix on any
particular schedule, and assume nothing is embargoed indefinitely.

Please do not run automated scanners against anything but your own machine.
There is no hosted service to test — trenvy is a local binary.

## What is in scope

Anything that causes trenvy to expose a secret more widely than the design
below intends. Concretely:

- a secret value reaching `argv`, a log line, a terminal, or an error message;
- a rendered file written somewhere git would track it, or with permissions
  wider than `0600`;
- a secret readable from trenvy's own bookkeeping (the manifest, the registry,
  the state file) without the keyring;
- one profile's or one project's secret being reachable under another's name;
- a child process receiving credentials the design says are stripped.

## What is out of scope

- **Anything that can already read your keyring.** See the limits below; this
  is the main one and it is by design.
- Compromise of the OS keyring itself, or of the platform tools trenvy drives
  (`security(1)`, Secret Service, Credential Manager).
- Physical access, a compromised account, malware running as you, or a
  malicious dependency of *your* project that trenvy hands an environment to.
- A rendered `.env` that you chose to write with `--allow-tracked` and then
  committed.
- Weak passphrases supplied to the file backend via `TRENVY_PASSPHRASE`.

## What trenvy protects

Each of these is a property of the code, not an aspiration.

**Secret values are never in the manifest.** The manifest records a variable's
name, whether it is secret, and any plain values. A secret's value lives only
in the keyring. The manifest, the registry and the state file are all safe to
read; none of them holds a secret.

**Presence checks do not cost an authorization prompt on macOS.** The store
interface separates `Has` from `Get` deliberately. `status` and the interactive
table build their rows from `Has`, so listing secrets never reads a value.
On macOS this is enforced by driving `security(1)` without `-w`, which answers
"does this exist?" silently.

**A masked table never opens the keyring at all.** `trenvy show` without
`--reveal` substitutes the mask for every secret and resolves plain variables
with a nil lookup function, so there is no keyring access to prompt for.

**Secrets never appear in `argv`.** `trenvy secret set` reads the value from a
no-echo terminal prompt, or from stdin with `--stdin`. `trenvy set` refuses a
variable marked secret outright rather than accepting a value on the command
line. This matters because `argv` is visible to any process on the machine via
`ps`, and lands in shell history. The success message reports the value's
length, never the value.

**Rendered files are `0600` and written atomically.** The write goes to a temp
file in the destination directory, is chmod'ed `0600`, synced, then renamed
into place, so there is no window in which a secret exists with default
permissions or half-written.

**`render` refuses a path git would track.** If `git check-ignore` says the
path is not ignored, the render is refused with the line to add to
`.gitignore`. `--allow-tracked` overrides it.

**`render` and `clean` refuse to clobber what they did not write.** A file with
no render record is `FOREIGN`; one whose bytes changed since trenvy wrote it is
`MODIFIED`. Both require `--force`.

**The state file's digest is keyed.** Staleness is detected with an HMAC over
the resolved values, keyed by a per-project salt generated at `project init` —
not a bare hash. An unkeyed digest of a low-entropy secret sitting in the state
file could be brute-forced, which would mean trenvy's own bookkeeping weakened
the secrets it exists to protect.

**`exec` strips `TRENVY_PASSPHRASE` from the child environment.** It unlocks
the file backend, and a child has no business inheriting it just because it
inherits everything else.

**The file backend is real encryption.** age with an scrypt passphrase
recipient, over the whole document. The secrets directory is created `0700`,
and writes go through the same atomic temp-and-rename path, so a crash cannot
destroy the only copy.

**Keyring namespacing is enforced.** Items are addressed
`trenvy/{project}/{profile}/{name}`. A custom `keyring.service` pattern that
omits `{profile}` is rejected when the config loads, because every profile
would otherwise share one item and a profile switch would silently read the
wrong credential.

## What trenvy does not protect against

Stated as plainly as the protections above.

**Anything that can read your keyring can read these secrets.** This is the
fundamental limit. trenvy stores credentials in the OS keyring and adds no
second factor, no passphrase of its own, and no per-item access control. Any
program running as you that can talk to the Keychain, the Secret Service or the
Credential Manager can read every secret trenvy stored, without going through
trenvy. If your user account is compromised, so are these secrets.

**A rendered file is plaintext on disk.** `render` writes real values. `0600`
means your user and root; it does not mean encrypted. Anything that can read
your home directory — a backup agent, a sync client, another tool running as
you — can read a rendered `.env`. If you do not need the file, prefer
`trenvy exec` or `trenvy shell`, which write nothing.

**Dotenv output is deliberately not safe to `source`.** `$` is left unescaped,
because the file is built for dotenv readers (docker compose `env_file:`,
DotNetEnv) that treat contents literally. Sourcing it in bash performs
parameter expansion inside the double quotes, so a value containing `$` is
silently corrupted, and a crafted value could expand to something you did not
intend. A value of `pa$$word-literal` sourced in bash becomes the shell's PID
spliced into the string. Use `trenvy exec` or `trenvy shell` instead of
sourcing rendered files.

**`exec` and `shell` put secrets in a process environment.** That environment
is inherited by every descendant, and on Linux is readable via `/proc` by the
same user. This is the normal way credentials reach a program, but it is not a
sandbox.

**Presence checks are only prompt-free on macOS.** On Linux and Windows the
system backend answers `Has` with a read, because the underlying library offers
nothing cheaper, so a presence check there can raise an unlock prompt. The file
backend answers `Has` by scrypt-decrypting the whole document. Shell completion
declines to probe on those backends rather than stalling or raising a dialog.

**`project rm` only reaches the currently selected backend.** Like every other
command it scopes to one backend. A secret stored under a different backend
survives — but the manifest that `project rm` deletes was that secret's only
inventory, so afterwards there is no record that it exists, or where.

**The git guard fails open.** If `git` is missing, or the directory is not a
repository, `GitIgnored` returns true and the render proceeds. The reasoning is
that nothing can be committed from a non-repository, so there is nothing to
protect against — but it does mean the guard is not a defence in a directory
that later becomes a repository.

**The file backend has no locking.** Every `Set` and `Delete` re-encrypts and
rewrites the whole document, so two concurrent writers against a shared file
backend can race, last writer winning.

**`trenvy set` values go in your shell history.** That is why secrets are
refused there. Plain values are, by definition, the ones you accepted are not
sensitive.

**There is no audit log.** trenvy does not record who read which secret when.

# Acceptance walkthrough

This is the spec's acceptance criteria, meant to be run by hand against your
real `demo-backend` and `demo-frontend` checkouts and your
real OS keyring. It is deliberately **not** part of CI or of any other
automated step:

- it writes real secret values into your real keyring,
- it writes files into your real working repositories,
- it creates a real git worktree next to one of them.

Run it yourself, a step at a time, and confirm each numbered criterion before
moving on. Nothing here needs root or sudo; it does need an unlocked login
keychain (or platform equivalent) and network access to your `psql` target.

**Stay in one shell for the whole walkthrough.** Every command below,
including the Frontend section and Teardown, assumes `TRENVY_CONFIG_DIR` is
still exported from the first command. Criteria 4 and 5 start a long-lived
`dotnet run` and an interactive `psql` session — natural moments to open a
second terminal — but a fresh shell has no `TRENVY_CONFIG_DIR`, and
`trenvy init` in that shell would register against your **real**
`~/.config/trenvy/registry.toml` instead of the scratch one. If you do need a
new terminal at any point, note the path printed below and re-export it
there first: `export TRENVY_CONFIG_DIR=<that path>`.

## Backend

```bash
export TRENVY_CONFIG_DIR=$(mktemp -d)
echo "$TRENVY_CONFIG_DIR"   # note this path in case you open a new terminal
go build -o /tmp/trenvy ./cmd/trenvy
cd ~/workspace/demo/demo-backend

/tmp/trenvy init --id vpb-test
/tmp/trenvy import main/src/DemoApp.Api/.env_example --target docker
/tmp/trenvy status                       # expect three MISSING secrets
/tmp/trenvy secret set BETA_TOKEN
/tmp/trenvy secret set GAMMA_TOKEN
/tmp/trenvy secret set ALPHA_TOKEN
/tmp/trenvy status                       # expect all present, docker not rendered

cd main
/tmp/trenvy render docker
diff <(grep -v '^#' src/DemoApp.Api/.env | sort) \
     <(grep -v '^#' src/DemoApp.Api/.env_example | sort) || true
/tmp/trenvy status                       # expect docker fresh
/tmp/trenvy clean docker
```

The `import` command above opens the interactive table instead of skipping
straight to the manifest. Review each row: confirm the three placeholder
values (`BETA_TOKEN`, `GAMMA_TOKEN`,
`ALPHA_TOKEN`) are marked secret (`s` toggles) and everything else is not,
then press `w` to save. This is what actually exercises criterion 1 below —
reviewing secret-ness is a TUI step, not something `--no-tui` can stand in
for.

Confirm, one criterion at a time:

1. the table let you review each variable's secret-ness before saving, and
   the saved manifest lists the same variables as `.env_example` with the
   three placeholders flagged secret
2. `status` reports every secret present after the three `secret set` calls
3. the rendered `.env` is `0600` and parses as the same key/value set
4. `trenvy exec local -- dotnet run --no-launch-profile --project src/DemoApp.Api`
   starts the API with no file written
5. `trenvy shell local`, then `psql "$APP_DB_CONNECTION"`, connects
6. `git worktree add ../FEAT-TEST && cd ../FEAT-TEST && /tmp/trenvy render docker`
   works with no setup
7. `trenvy edit`, change one value, save, then `status` reports the rendered
   file `STALE`

## Frontend

```bash
# Same shell as above — confirm with: echo "$TRENVY_CONFIG_DIR"
# If it's empty (new terminal), re-export the path you noted earlier first.
cd ~/workspace/demo/demo-frontend
/tmp/trenvy init --id vpfe-test
/tmp/trenvy import main/public/env.json.example --no-tui --target web
cd main && /tmp/trenvy render web && cat public/env.json
```

Confirm: five plain variables, no keyring use at all, and `public/env.json` is
byte-clean flat JSON that `src/main.tsx` can load.

## Tear down

The `secret set` calls in the Backend section wrote persistent entries into
your **real** OS keychain — `TRENVY_CONFIG_DIR` sandboxes only the config
directory, not the keyring. Remove them, and the test worktree, before
discarding the scratch config; once it's gone, so is the only trenvy-mediated
route back to those entries.

Still in the same shell, with `TRENVY_CONFIG_DIR` still exported:

```bash
cd ~/workspace/demo/demo-backend
/tmp/trenvy secret rm BETA_TOKEN --yes
/tmp/trenvy secret rm GAMMA_TOKEN --yes
/tmp/trenvy secret rm ALPHA_TOKEN --yes

cd main
git worktree remove ../FEAT-TEST

rm -rf "$TRENVY_CONFIG_DIR"
```

If the scratch config is already gone or `secret rm` isn't an option, delete
the entries directly instead: the service-name pattern is
`trenvy/{project}/{name}`, so for this walkthrough the exact keychain
services are `trenvy/vpb-test/BETA_TOKEN`,
`trenvy/vpb-test/GAMMA_TOKEN`, and
`trenvy/vpb-test/ALPHA_TOKEN` — find and remove these in Keychain Access
(or your platform's Secret Service / Credential Manager UI).

This leaves your real `~/.config/trenvy` (if any) untouched, removes the
scratch config directory, the `../FEAT-TEST` worktree, and the three keychain
entries created above. Once you've confirmed all criteria above, the real
`mise.toml` in these repositories is redundant and can be retired.

If any criterion fails, do not paper over it: open the spec, decide whether
the spec or the code is wrong, fix that one thing, and re-run the
walkthrough.

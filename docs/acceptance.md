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

## Backend

```bash
export ENVCTL_CONFIG_DIR=$(mktemp -d)
go build -o /tmp/envctl ./cmd/envctl
cd ~/workspace/demo/demo-backend

/tmp/envctl init --id vpb-test
/tmp/envctl import main/src/DemoApp.Api/.env_example --no-tui --target docker
/tmp/envctl status                       # expect three MISSING secrets
/tmp/envctl secret set BETA_TOKEN
/tmp/envctl secret set GAMMA_TOKEN
/tmp/envctl secret set ALPHA_TOKEN
/tmp/envctl status                       # expect all present, docker not rendered

cd main
/tmp/envctl render docker
diff <(grep -v '^#' src/DemoApp.Api/.env | sort) \
     <(grep -v '^#' src/DemoApp.Api/.env_example | sort) || true
/tmp/envctl status                       # expect docker fresh
/tmp/envctl clean docker
```

Confirm, one criterion at a time:

1. the imported manifest lists the same variables as `.env_example`, with the
   three placeholders flagged secret
2. `status` reports every secret present after the three `secret set` calls
3. the rendered `.env` is `0600` and parses as the same key/value set
4. `envctl exec local -- dotnet run --no-launch-profile --project src/DemoApp.Api`
   starts the API with no file written
5. `envctl shell local`, then `psql "$APP_DB_CONNECTION"`, connects
6. `git worktree add ../FEAT-TEST && cd ../FEAT-TEST && /tmp/envctl render docker`
   works with no setup
7. `envctl edit`, change one value, save, then `status` reports the rendered
   file `STALE`

## Frontend

```bash
cd ~/workspace/demo/demo-frontend
/tmp/envctl init --id vpfe-test
/tmp/envctl import main/public/env.json.example --no-tui --target web
cd main && /tmp/envctl render web && cat public/env.json
```

Confirm: five plain variables, no keyring use at all, and `public/env.json` is
byte-clean flat JSON that `src/main.tsx` can load.

## Tear down

```bash
rm -rf "$ENVCTL_CONFIG_DIR"
```

This removes only the scratch config directory created above; your real
`~/.config/envctl` (if any) and your real keyring entries are untouched. Once
you've confirmed all criteria above, the real `mise.toml` in these
repositories is redundant and can be retired.

If any criterion fails, do not paper over it: open the spec, decide whether
the spec or the code is wrong, fix that one thing, and re-run the
walkthrough.

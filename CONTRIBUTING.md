# Contributing

## Build and test

    go build ./...
    go vet ./...
    go test ./...                 # no root, no cloud needed
    GOOS=windows go build ./...   # the operator client must keep building for Windows

Before sending a change, also run the checks CI runs: `staticcheck ./...`, `gosec -exclude-dir=prototype ./...`,
`govulncheck ./...`, and a secret scan. **Never commit credentials, keys, state files or real account/resource ids.**
Use placeholders in docs and tests.

## Where things go

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Pure logic gets unit tests with injected fakes (`provider/fake`,
the `Runner` interface in `internal/hub`). Anything that runs commands goes through those seams, so tests don't need root.

## Real-cloud tests (read before running)

`prototype/aws-go/` holds scripts that **create real AWS resources** and cost money:
`rig-up.sh`, the `*-test.sh` scripts and `rig-down.sh`. Rules:

* Never run them in an account you don't have permission to spend in, and **never in a shared account without
  asking the account's owners first.**
* Always finish with `rig-down.sh` and confirm its verification section prints all zeros.
* Never put real ids or keys into files you commit. Rig working directories are git-ignored.

## Style

* Operator-facing messages are plain language and tell the person what to do next. No jargon.
* Fail closed: when something is wrong, block traffic rather than leak.
* Idempotent, level-triggered changes over one-shot scripts.
* Mark unverified claims about cloud platforms as such in docs.

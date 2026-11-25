# Contributing

## Build and test

    go build ./...
    go vet ./...
    go test ./...                 # needs no root and no cloud account
    GOOS=windows go build ./...   # the tester client must keep building for Windows

Before opening a pull request, also run the checks CI runs: `staticcheck ./...`, `gosec -exclude-dir=prototype ./...`,
`govulncheck ./...`, and a secret scan. Never commit credentials, keys, state files, or real account or resource IDs.
Use placeholders in docs and tests.

## Code layout and testing

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) has the code map. Pure logic gets unit tests with injected fakes (the
`provider/fake` package and the `Runner` interface in `internal/hub`). Anything that runs commands goes through those
interfaces, so tests do not need root.

## Real-cloud tests

`prototype/aws-go/` holds scripts that create real AWS resources and cost money. They are `rig-up.sh`, the `*-test.sh`
scripts, and `rig-down.sh`. Follow these rules.

* Do not run them in an account you are not authorized to spend in, and never in a shared account without asking its
  owners first.
* Finish every run with `rig-down.sh`, and confirm the verification section reports no remaining instances, volumes,
  key pairs, or roles. The network that `niblr init` created stays in place.
* Never put real IDs or keys into files you commit. Rig working directories are excluded by `.gitignore`.

## Style

* Write messages for testers in plain language, and say what to do next.
* Fail closed. When something is wrong, block traffic instead of letting it leak.
* Prefer idempotent, level-triggered reconciliation (act on the current state, not on events) over one-shot scripts.
* Label unverified claims about cloud platforms as unverified in docs.

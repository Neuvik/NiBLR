# Security

NiBLR runs infrastructure that sends traffic from many addresses. Use it only where you have written authorization to
test.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting under this repository's Security tab,
or email the maintainers at Neuvik. A dedicated security address will be added here before the repository is published.

## Secrets and credentials

* No credentials, keys, or state files belong in this repository. `.gitignore` excludes common patterns, and CI scans
  the full git history for secrets.
* Real cloud account numbers, resource IDs, and keys are never committed. The AWS policy in `docs/IAM-aws.md` uses
  placeholders.
* A tester's private key is generated on the tester's machine and never sent anywhere.

## Checks on every change

CI runs `go vet`, `staticcheck`, `gosec`, `govulncheck`, `go test`, and a gitleaks scan of the full git history. See
`.github/workflows/ci.yml`.

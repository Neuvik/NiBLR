# Security

NiBLR runs infrastructure that sends traffic from many addresses. Treat it like a tool that can do real damage in
the wrong hands, and only use it where you have written authorization to test.

## Reporting a vulnerability

Please do not open a public issue. Email the maintainers at Neuvik (security contact on neuvik.com) or use GitHub's
private vulnerability reporting on this repository.

## What is and is not in this repository

* No credentials, keys or state files belong here. `.gitignore` blocks the usual ones and CI runs a secret scan.
* Real cloud accounts, resource ids and keys are never committed. The AWS policy in `docs/IAM-aws.md` uses placeholders.
* Operator private keys are generated on the operator's machine and never sent anywhere.

## Checks that run on every change

`go vet`, `staticcheck`, `gosec`, `govulncheck`, `go test`, and a secret scan of the whole git history (gitleaks in CI; betterleaks was also run locally).
See `.github/workflows/ci.yml`.

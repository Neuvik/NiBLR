# Releasing NiBLR

## Versioning

NiBLR uses [Semantic Versioning](https://semver.org/). Versions look like `v0.1.0`.

* Before 1.0.0, a minor version (0.x.0) may change commands, flags, or the invite format. Patch versions (0.x.y) are bug
  and security fixes only.
* From 1.0.0, breaking changes need a major version. An invite format change counts as breaking, because old invites stop
  working.
* Releases before 1.0.0 are marked as pre-releases on GitHub.

The version is set at build time. `niblr version` and `niblrd version` print it. A plain `go build` reports `dev`.

## Cutting a release

1. Move the entries under `## [Unreleased]` in `CHANGELOG.md` into a new `## [x.y.z] - YYYY-MM-DD` section, and update the
   comparison links at the bottom. Write notes for testers and admins, and list known limitations honestly.
2. Run the checks from [CONTRIBUTING.md](../CONTRIBUTING.md) and confirm they pass.
3. Commit the changelog, then tag and push:

       git tag -a vX.Y.Z -m "NiBLR vX.Y.Z"
       git push origin main vX.Y.Z

4. The `release` workflow builds the binaries, writes `SHA256SUMS`, uses the matching changelog section as the release
   notes, and publishes the GitHub release. It fails if the changelog has no section for the tag.

## What a release contains

* `niblr` for Linux (amd64, arm64) and Windows (amd64)
* `niblrd` for Linux (amd64, arm64)
* `SHA256SUMS`

The binaries are not code-signed, so Windows may warn that the publisher is unknown. Testers should check the SHA-256
checksum against `SHA256SUMS` from a trusted copy of the release page.

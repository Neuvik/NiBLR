# Releasing NiBLR

## Versioning

NiBLR versions are numbered by date, as `YEAR.MONTH.PATCH`. For example, `v2026.11.0` is the first release in November
2026, and `v2026.12.0` is the first release in December.

* The first number is the year of the release, and the second is the month, written without a leading zero (`2026.9.0`,
  not `2026.09.0`), so the tags stay valid semantic versions.
* The third number counts releases within that month. It starts at 0, so a second release in November 2026 is
  `v2026.11.1`. Fixes to a shipped release use the same counter.

The number does not promise compatibility, so every release's notes list anything that breaks under "Breaking". That
includes changed commands or flags, and any change to the invite format, because old invites stop working. Read the notes
before upgrading the hub or testers' programs, and keep the hub and the testers on the same release.

A tag with a hyphen, such as `v2026.12.0-rc1`, is published as a GitHub pre-release. Plain tags are normal releases.

The version is set at build time. `niblr version` and `niblrd version` print it. A plain `go build` reports `dev`.

## Cutting a release

1. Move the entries under `## [Unreleased]` in `CHANGELOG.md` into a new `## [YEAR.MONTH.PATCH] - YYYY-MM-DD` section, and update the
   comparison links at the bottom. Write notes for testers and admins, and list known limitations honestly.
2. Run the checks from [CONTRIBUTING.md](../CONTRIBUTING.md) and confirm they pass.
3. Commit the changelog, then tag and push:

       git tag -a vYEAR.MONTH.PATCH -m "NiBLR vYEAR.MONTH.PATCH"
       git push origin main vYEAR.MONTH.PATCH

4. The `release` workflow builds the binaries, writes `SHA256SUMS`, uses the matching changelog section as the release
   notes, and publishes the GitHub release. It fails if the changelog has no section for the tag.

## What a release contains

* `niblr` for Linux (amd64, arm64) and Windows (amd64)
* `niblrd` for Linux (amd64, arm64)
* `SHA256SUMS`

The binaries are not code-signed, so Windows may warn that the publisher is unknown. Testers should check the SHA-256
checksum against `SHA256SUMS` from a trusted copy of the release page.

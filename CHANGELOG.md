# Changelog

All notable changes to NiBLR are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/). Before 1.0.0, a minor version (0.x) may change commands,
flags, or the invite format, and the notes will say when it does.

## [Unreleased]

## [0.1.0] - 2026-11-25

First public preview. NiBLR is a complete rewrite of the earlier Python, Terraform, and Ansible tool, which has been
removed. This release works end to end on AWS and has several known gaps, listed below.

### Added

* `niblr`, the command-line tool. Testers use `join`, `status`, and `leave` on Linux and Windows. Admins use `init` to
  create the AWS network. `version` shows the build.
* `niblrd`, the hub daemon. It builds and maintains a pool of exits on AWS (`--want N`), routes each new tester
  connection to a random healthy exit, health-checks every exit end to end, and replaces exits that are deleted or fail.
* One-time, expiring invites (`niblrd invite`). The tester's key is generated on their machine, and enrollment uses TLS
  pinned to the hub's certificate fingerprint.
* A tester client that routes all IPv4 and IPv6 traffic into the tunnel, sends DNS through it, and on Linux blocks
  traffic outside it.
* Optional exit leases (`--lease-minutes`), so a forgotten exit shuts itself down and terminates.
* A least-privilege IAM policy for the hub, safe for a shared AWS account (`docs/IAM-aws.md`).
* Documentation: tester guide, admin guide, troubleshooting, cost and cleanup, acceptable use, architecture, and design
  notes.
* Test scripts that exercise the system on real AWS (`prototype/aws-go/`).

### Changed

* `niblr join` checks for administrator rights, WireGuard, and an existing join before it uses the invite, so those
  mistakes no longer use up a one-time invite.

### Security

* Built with Go 1.26.11 to pick up fixes for known standard-library vulnerabilities.
* The enrollment client verifies the hub's certificate pin on every connection, including resumed sessions.
* A WireGuard key read from a cloud tag is validated before it reaches a command line.

### Known limitations

* The hub does not block tester traffic to private and cloud metadata addresses.
* The Windows client has not been tested on a real Windows machine. The Linux client has been tested on Ubuntu 24.04 only.
* IPv6 is blocked rather than carried, and its behavior on a dual-stack machine has not been measured.
* A rebuilt hub needs the same public address, and testers need new invites. There is no command to revoke a tester.
* Only AWS is supported. A site that ties a login to one source address can drop a session, because a session can open
  connections through different exits.
* The hub is set up by hand. There is no installer or machine image.

[Unreleased]: https://github.com/Neuvik/NiBLR/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Neuvik/NiBLR/releases/tag/v0.1.0

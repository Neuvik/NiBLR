<div align="center">

```
      ,_,
     (o,o)      N i B L R
     {`"'}      Neuvik's IP BLock Rotator
     -"-"-
```

### One tunnel in, many addresses out

*Named after Nibbler from Futurama.*

`v2026.11.0` · `Go 1.26` · `WireGuard` · `Linux and Windows testers` · `AWS only for now`

</div>

---

## What it does

Defenses on a target often react when a single address sends too much traffic. NiBLR gives a tester one
connection that leaves from many public addresses and rotates them as the work goes on.

```
  you ──WireGuard──▶  hub ──▶ exit 1 ──▶ 203.0.113.10 ─┐
 (all your            │  ├──▶ exit 2 ──▶ 198.51.100.7  ├──▶  the target
  traffic)            │  └──▶ exit 3 ──▶ 192.0.2.44   ─┘
                      └─ picks an exit for each NEW connection and keeps it until that connection ends
```

* It works for any IPv4 protocol, outbound only. Web, SSH, scanners, ping, and DNS all work with no per-tool settings.
  Exits accept no inbound connections, so reverse shells and callbacks do not work. IPv6 is blocked.
* Each new connection picks a random healthy exit, and a single connection keeps its exit until it ends. A site that
  ties a login to one source address can still drop a session that opens a second connection from another exit.
* Deleting an exit gets you a fresh address. The hub builds a replacement without being asked.
* If no exit is healthy, the hub drops new connections. They never leave from your own address.
* The hub also drops new connections to private and cloud metadata addresses, so an exit cannot be used to reach the
  network around it.

## For testers

You need an invite from your admin. It starts with `niblr1:`.

```
niblr join niblr1:eyJ...       # all your traffic now goes through NiBLR
niblr status                   # are you connected, and what address do sites see?
niblr leave                    # disconnect and put everything back
```

Those three commands are all most testers need. The details are in the [tester guide](docs/OPERATOR-GUIDE.md).

* There are no keys to copy, no address ranges, and no config files.
* Linux and Windows are supported. Windows needs WireGuard for Windows installed and an Administrator terminal.
  If WireGuard is missing, `niblr` says so.
* While you are joined, DNS goes through the tunnel, IPv6 is blocked, and on Linux a firewall rule blocks anything that
  tries to skip the tunnel. If the tunnel drops, your traffic stops instead of leaking.
* An invite works once and expires. If a join fails, the message says why. Ask your admin for a new invite if it says
  the invite was used.

## For admins

### How the pieces fit

| Piece | What it is | Where it runs |
|---|---|---|
| `niblr` | The command-line tool (`init`, `join`, `leave`, `status`) | Tester laptops and the admin workstation |
| `niblrd` | The hub daemon. It builds the exit pool, routes traffic, checks health, and enrolls testers | One small VM in AWS |
| Exits | Small cloud VMs (`t3.nano`) that each connect back to the hub over WireGuard | AWS, disposable |

There is no Terraform, no Ansible, and no state file for exits. Every exit carries tags (`niblr-managed`, `niblr-env`,
`niblr-slot`, and its public key), and the hub keeps comparing what should exist with what does exist. The hub does keep
a little local state (its key and the list of enrolled testers), covered in the admin guide.

### Quick start

```bash
# 1. Build the binaries
go build -o bin/ ./cmd/...

# 2. Create the network pieces in your AWS account (a dedicated VPC and security groups, all tagged)
bin/niblr init --env myteam --region us-west-1

# 3. Launch and configure the hub (IAM role, hub key, systemd service). See the admin guide, steps 3 to 5.
#    The daemon then builds and maintains the exit pool:
#    niblrd --want 3 --env myteam --region us-west-1 --ami <ami> --subnet <subnet> --exit-sg <sg> --hub-ip <ip>

# 4. On the hub, create an invite for a tester (valid for 24 hours by default, usable once)
niblrd invite --name alice --hub-ip <hub-public-ip>
```

Give the printed `niblr1:...` string to the tester. The [admin guide](docs/ADMIN-GUIDE.md) has every step.

> **Status:** these steps have been run on a real AWS account using the scripts in `prototype/aws-go/`, but the hub
> is still set up by hand. There is no installer or machine image yet.

### Safety on a shared AWS account

NiBLR is designed to run in an AWS account that also holds unrelated resources.

* The hub's role can launch only into NiBLR's own subnet and security group, only small instance types, and only
  touch instances that carry NiBLR's tags (`niblr-managed=true` and its own `niblr-env`).
* The role cannot pass roles, tag instances it did not create, or use wildcard actions.
* The daemon checks the tags again before every delete, as a second lock behind IAM.
* Exits can carry a lease (`--lease-minutes`). When it is set, a forgotten exit shuts itself down and terminates.

The full policy and its remaining risks are in [`docs/IAM-aws.md`](docs/IAM-aws.md).

### How enrollment stays safe

The tester's private key is generated on the tester's machine and never leaves it. Joining sends only the public key
and the one-time token, over TLS pinned to the hub certificate's fingerprint (carried in the invite). The hub refuses
a reused invite, and the client refuses a certificate that does not match. [`docs/DESIGN.md`](docs/DESIGN.md) has the
details.

## What has and has not been tested

Tested on real AWS, with scripts in `prototype/aws-go/`:

* The pool built itself from nothing (three exits in about a minute in one run), and connections spread across all exits.
* Rotation worked while traffic was running. The test allows up to 5 percent failed flows, and the last recorded run had none.
* The hub rebuilt its tunnel interfaces from cloud tags after they were wiped.
* Scale-down worked, and all 17 IAM permission checks passed.
* Traffic to private and cloud metadata addresses is blocked at the hub. A joined tester could not reach a private
  address in the same VPC or the metadata address, though both answered when reached directly.
* A real tester join, the leak checks, the kill switch, and recovery worked on Ubuntu 24.04. A reused invite and a
  wrong certificate were both refused.

Not tested yet:

* Windows on a real Windows machine. It builds, but the kill switch has not been exercised.
* Linux distributions other than Ubuntu 24.04.
* IPv6 on a real dual-stack machine. The configuration is written, but its behavior, including how quickly tools fall
  back to IPv4, has not been measured.
* Replacing the hub VM itself. Only the interface rebuild was tested.
* Heavy load near the connection-tracking limit.
* Revoking a tester. There is no command for it yet, and a rebuilt hub means re-inviting testers.

## Where it is going

These are ideas, roughly in priority order. None are built.

1. Exits in other clouds, starting with GCP and then Azure, using the same data path.
2. A proxy mode (SOCKS5 and HTTPS) for people who do not want a full tunnel. Lightweight serverless workers (Lambda,
   Cloud Run) would do the work, and only the hub would be able to call them. They would never be an open proxy.
3. Keyless access to other clouds through workload identity federation instead of stored keys.

[`docs/DESIGN.md`](docs/DESIGN.md) explains the reasoning.

## Documentation

| Read this | If you want to |
|---|---|
| [Tester guide](docs/OPERATOR-GUIDE.md) | join NiBLR on Linux or Windows |
| [Troubleshooting and FAQ](docs/TROUBLESHOOTING.md) | fix an error message or find a quick answer |
| [Admin guide](docs/ADMIN-GUIDE.md) | set up and run an environment |
| [Cost and cleanup](docs/COST-AND-CLEANUP.md) | understand the bill or tear everything down |
| [Acceptable use](docs/ACCEPTABLE-USE.md) | read this before first use |
| [Architecture](docs/ARCHITECTURE.md) | understand how it works |
| [IAM policy](docs/IAM-aws.md) | review the hub's AWS permissions |
| [Design notes](docs/DESIGN.md) | see why it is built this way and where it is going |
| [Contributing](CONTRIBUTING.md) | change the code |
| [Releasing](docs/RELEASING.md) and [Changelog](CHANGELOG.md) | see what changed and how releases are made |

## Repository map

```
cmd/niblr/        the tester and admin command-line tool
cmd/niblrd/       the hub daemon
internal/         cloud provider interface, AWS provider, reconciler, hub routing, enrollment, client setup
prototype/        scripts that exercise the system on real AWS
docs/             guides, architecture, design notes, and the IAM policy
```

---

<div align="center">

*Built by [Neuvik](https://neuvik.com).*

</div>

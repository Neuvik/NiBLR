<div align="center">

```
      ,_,
     (o,o)      N i B L R
     {`"'}      Neuvik's IP BLock Rotator
     -"-"-
```

### One tunnel in. A different address out. Every time.

*Named for Lord Nibbler, the little guy who looks harmless and quietly does the heavy lifting.*

`Go 1.24` · `WireGuard` · `Linux + Windows operators` · `AWS today, more clouds on the roadmap`

</div>

---

## What it does

You are a tester. The target's defenses notice when one address hits them too often.
NiBLR gives you **one connection** that comes out of **many public addresses**, rotating as you go, so your
traffic looks like a crowd instead of a single noisy person.

```
  you ──WireGuard──▶  hub ──▶ exit 1 ──▶ 203.0.113.10 ─┐
 (all your            │  ├──▶ exit 2 ──▶ 198.51.100.7  ├──▶  the target
  traffic)            │  └──▶ exit 3 ──▶ 192.0.2.44   ─┘
                      └─ picks an exit for each NEW connection, remembers it for that connection's life
```

* **Full L3.** Any protocol: web, SSH, scanners, ping, DNS. Nothing to configure per tool.
* **Rotation that does not break things.** Each new connection picks a random healthy exit. A connection stays on
  its exit until it ends, so logins and sessions do not fall apart halfway through.
* **Replace an address on demand.** Deleting an exit gets you a fresh address and the hub rebuilds the slot by itself.
  The last test did this with zero failed connections during the swap.
* **No address, no traffic.** If no exit is healthy, new connections are dropped. They are never sent out of your own
  address by accident.

## For operators (the people testing)

You need one thing from your admin: an **invite** that looks like `niblr1:eyJ...`.

```
niblr join niblr1:eyJ...       # connect. All your traffic now goes through NiBLR.
niblr status                   # am I connected? (plain-language answer)
niblr leave                    # disconnect and put everything back
```

That is the whole manual. Specifically:

* **Nothing to configure.** No keys to copy, no IP ranges, no config files.
* **Linux and Windows.** On Windows, run from an Administrator terminal. WireGuard for Windows must be installed; if it
  is not, `niblr` tells you so in plain words.
* **Your real address stays hidden.** DNS goes through the tunnel, IPv6 is routed into the tunnel (and dropped there),
  and on Linux a firewall rule blocks anything that tries to leave outside the tunnel. If the tunnel drops, traffic
  stops instead of leaking.
* **Invites are single-use** and expire. If one fails, ask your admin for a new one; the error will say why.

## For admins

### How it fits together

| Piece | What it is | Where it runs |
|---|---|---|
| `niblr` | The operator CLI (`init`, `join`, `leave`, `status`) | Operator laptops, admin workstation |
| `niblrd` | The hub daemon: builds the exit pool, routes, health-checks, enrolls operators | One small VM in AWS |
| Exits | Tiny cloud VMs (`t3.nano`) that each dial back to the hub over WireGuard | AWS, disposable |

There is **no Terraform, no Ansible and no state file.** The cloud itself is the source of truth: every exit carries
tags (`niblr-managed`, `niblr-env`, `niblr-slot`, its public key), and the hub continuously compares "what should exist"
with "what does exist" and fixes the difference. Lose the hub, build a new one, and it finds its exits again from the tags.

### Quick start

```bash
# 1. Build the binaries
go build -o bin/ ./cmd/...

# 2. Create the network pieces in your AWS account (dedicated VPC, security groups, all tagged)
bin/niblr init --env myteam --region us-west-1

# 3. On the hub VM, run the daemon. It builds and keeps the exit pool all by itself.
bin/niblrd --want 3 --env myteam --region us-west-1 \
    --ami <ubuntu-ami> --subnet <subnet-id> --exit-sg <sg-id> --hub-ip <hub-public-ip>

# 4. Mint an invite for an operator (run on the hub; valid for a limited time, usable once)
bin/niblrd invite --name alice --ttl 24h --hub-ip <hub-public-ip>
```

Hand the printed `niblr1:...` string to the operator. Done.

> **Honest status:** steps 2 to 4 are proven on a real AWS account via the rig scripts in `prototype/aws-go/`, but
> the hub's own packaging (an image or install script) is not written yet. `--help` on each binary lists every flag.

### Safety on a shared AWS account

NiBLR was built to live in an account that also holds other people's things.

* The hub's role can only launch into NiBLR's own subnet and security group, only small instance types, and only
  touch instances carrying **its** tags (`niblr-managed=true` and its own `niblr-env`).
* It cannot pass roles, tag other people's instances, or use any wildcard action.
* The daemon re-checks tags itself before every delete, as a second lock behind IAM.
* Exits can carry a **lease** (`--lease-minutes`): a forgotten exit shuts itself down and terminates.

The full policy, and the residual risks to know about, are in [`docs/IAM-aws.md`](docs/IAM-aws.md).

### How enrollment stays safe

The operator's private key is made **on the operator's machine** and never leaves it. Joining sends only the public key
plus the one-time token, over TLS pinned to the hub's certificate fingerprint (carried inside the invite). A reused
invite is refused. So is a wrong certificate. Details in [`docs/DESIGN.md`](docs/DESIGN.md).

## What has been proven, and what has not

We would rather tell you than have you find out at 2am.

**Tested on real AWS:**
* Pool built from nothing in about a minute; connections spread across all exits.
* Rotation with no failed connections; hub rebuilt from tags alone; scale-down; 17 of 17 IAM permission checks.
* Real operator join, leak checks, kill switch and recovery on Linux; reused and wrong-certificate invites refused.

**Not yet tested (be careful):**
* **Windows** on a real Windows machine. It builds, but the kill switch is untested.
* **IPv6** on a real dual-stack machine. The config is right; the behavior (and speed of fallback to IPv4) is unmeasured.
* Heavy load near the connection-tracking limit.
* Revoking an operator (there is no command yet). A rebuilt hub means re-inviting operators.

## Where it is going

Ideas, in rough order. **None are built yet**, and the owner is still deciding what stays:

1. **More clouds** for exits: GCP, then Azure, sharing the same data plane.
2. **Proxy mode** (SOCKS5/HTTPS) for people who do not want a full tunnel, backed by lightweight serverless workers
   (Lambda, Cloud Run) that stay **private**, called only by the hub. Never an open proxy.
3. **Keyless cross-cloud access** through workload identity federation instead of stored keys.

The reasoning for each is written up in [`docs/DESIGN.md`](docs/DESIGN.md).

## Documentation

| Read this | If you are |
|---|---|
| [Operator guide](docs/OPERATOR-GUIDE.md) | a tester joining NiBLR (Linux or Windows) |
| [Troubleshooting and FAQ](docs/TROUBLESHOOTING.md) | seeing an error message, or wondering how something works |
| [Admin guide](docs/ADMIN-GUIDE.md) | standing up and running an environment |
| [Cost and cleanup](docs/COST-AND-CLEANUP.md) | worried about the bill or tearing down |
| [Acceptable use](docs/ACCEPTABLE-USE.md) | anyone, before first use |
| [Architecture](docs/ARCHITECTURE.md) | a developer, or just curious how it works |
| [IAM policy](docs/IAM-aws.md) | reviewing the hub's AWS permissions |
| [Design notes](docs/DESIGN.md) | wondering why it is built this way, and where it is going |
| [Contributing](CONTRIBUTING.md) | changing the code |

## Repository map

```
cmd/niblr/        operator + admin CLI
cmd/niblrd/       hub daemon
internal/         provider contract, AWS provider, reconciler, hub routing, enrollment, client setup
prototype/        the rig scripts that proved it on real AWS
docs/             DESIGN.md (why it works this way), IAM-aws.md (the locked-down role)
```

> The old Python/Terraform/Ansible version (`main.py`, `terraform/`, `ansible/`, `setup.sh`, ...) is still in the
> tree until the new version is declared proven, then it goes. Do not use it.

---

<div align="center">

*Built by [Neuvik](https://neuvik.com). Good news, everyone: you now come from everywhere.*

</div>

# NiBLR design

This page records why NiBLR is built the way it is, what is decided, and what is planned. [ARCHITECTURE.md](ARCHITECTURE.md)
explains how it works today.

The goal is that a tester runs `niblr join`, gets a full layer-3 connection (any IP protocol), and their traffic leaves
from rotating public addresses. Testers are not network specialists, so there is nothing to configure, errors are in
plain language, and there are no proxy settings.

## Shape

NiBLR is one Go module that uses the cloud SDKs directly. It does not use Terraform, Pulumi, or Ansible.

* `cmd/niblr` is the tester and admin tool (`init`, `join`, `leave`, `status`, and `dev` helpers). Commands for
  rotating, scaling, and tearing down an environment are planned and not implemented.
* `cmd/niblrd` is the hub daemon (health checks, exit pool, rotation, and the enrollment API).
* `internal/provider` is the cloud contract, with one small implementation per cloud.
* `internal/pool`, `internal/dataplane`, and `internal/health` hold the hub's logic, which is unit-tested without root.

NiBLR avoids Terraform and Pulumi because testers would have to install them, rotation is imperative (create, replace,
and re-IP in a loop), and `join` needs idempotent discovery. State lives in cloud tags and on the hub, so there is no
state file for exits to lose.

## Data plane

This design is proven locally and on AWS (see `prototype/`).

* A tester is a WireGuard peer on the hub with its own `/32` and key.
* Each exit has its own WireGuard interface on the hub. Exits connect to the hub, so an exit's public IP can change.
* nftables marks each new tester flow with an exit ID stored in conntrack, and `ip rule fwmark` selects the exit's
  route table. Every packet of a flow uses the same exit in both directions.
* A health loop rewrites the marking rules atomically. With no healthy exit, the hub drops new flows (fail closed).
* An exit is healthy when it has a recent handshake and passes an end-to-end probe (a marked ping from the tester subnet,
  out through the exit and back). A tunnel-only probe is not enough, because it kept routing into an exit that had lost
  its return route.
* Draining an exit (it leaves the pool and its tunnel stays up) never breaks established flows.

### Exit types

| Type | What it is | Full L3 | Notes |
|---|---|---|---|
| `vm` | A small VM per exit that connects to the hub | Yes | Works on any provider. Implemented on AWS. |
| `hub-ip` | Extra private IPs on the hub mapped one to one to public IPs, chosen by `nft snat` | Yes | One box with many addresses. Untested. Not possible on GCP, which allows one external IP per network interface. |
| `proxy` | Container or function exits | TCP through SOCKS or HTTP only | Planned and optional. |

### Operational notes

* `wg-quick@` units hit systemd start limits when restarted more than five times in ten seconds, so exit units set
  `Restart=on-failure` and a relaxed limit.
* Per-flow rotation can break targets that pin sessions by source IP, so the rotation policy should be configurable.
  It is not configurable today.

## Requirements and open items

Lifecycle:

* Creates set their tags atomically (`TagSpecifications`) and are idempotent (`ClientToken`), so a half-finished create
  is never an untagged, billed orphan. Both are implemented.
* The reconcile loop on the hub removes strays. An out-of-band `niblr down` that sweeps by environment tag without the
  hub is planned.
* Leases keep a dead hub from leaving exits running. On AWS this is instance-initiated shutdown with terminate
  behavior, and it is implemented and optional. GCP has `maxRunDuration` *(verify)*.
* A mark ID is never reused without `conntrack -D --mark N` first. The ordering rule is that a route and interface exist
  before an ID enters the map, and the ID leaves the map before teardown.

Security of the hub, which faces the internet:

* The hub's cloud role is scoped to the minimum. It has no `iam:PassRole` (exits get no instance profile),
  `ec2:CreateTags` only with `ec2:CreateAction=RunInstances`, and IMDSv2 required. A dedicated account or service
  control policy and a budget alarm are the only hard ceiling.
* `vm` exits use ephemeral public IPs because they connect outward. Elastic IPs are for the hub endpoint (and the
  `hub-ip` type). Each exit has the hub's address built in, so a hub that changes address strands its exits.
* The hub drops new tester flows to link-local and cloud metadata addresses (`169.254.169.254`, `168.63.129.16`),
  RFC1918, CGNAT, loopback, multicast, and reserved ranges before it chooses an exit. Without this, a tester could use
  an exit to reach private addresses next to it. The list is `PrivateRanges` in `internal/dataplane/rules.go`. It covers
  IPv4 only, because the hub carries no IPv6. Established flows are not affected.

Data plane traps:

* Conntrack can fill up under scanning. The hub setup raises `nf_conntrack_max`, while timeouts and alerting on table
  fill are still to do. Testing has reached about 5,000 entries.
* MTU and MSS matter because tunnels chain at the hub. The measured path MTU is 1420 (a 1392-byte payload passes and a
  1400-byte payload fails). Per-provider MTUs (GCP defaults to 1460) and MSS clamping still need work.
* `rp_filter` is set to 0 on the hub.
* The hub is disposable, not highly available. Its exit configuration is derivable from cloud tags, but the enrolled
  tester list lives on its disk, so a rebuilt hub means re-inviting testers. Use a stable endpoint (an Elastic IP) and
  keep the hub key. One hub per engagement limits the blast radius. Do not use a `t3.nano` beyond tests, because its
  burstable network credits run out under load.

## Shared AWS accounts

NiBLR can run in an AWS account that also holds unrelated resources, so the hub is scoped to its own network and tags.
[IAM-aws.md](IAM-aws.md) has the policy and preconditions. The reconciler never deletes an instance that lacks the
`niblr-managed` tag and its own `niblr-env`, even if it looks like a stray. The policy was tested against a real account
with `prototype/aws-go/iam-test.sh`.

## Tester client (Linux and Windows)

`niblr join` writes a WireGuard config and brings the tunnel up using the official client on each OS. The config always
routes `0.0.0.0/0` and `::/0` and sets DNS to `1.1.1.1` and `9.9.9.9` through the tunnel, so nothing leaks around it.

* On Windows, `niblr` installs the tunnel with WireGuard for Windows (`wireguard.exe /installtunnelservice`, which needs
  an administrator prompt). With both address families routed, WireGuard's "block untunneled traffic" kill switch is
  active.
* On Linux, `niblr` uses `wg-quick` with PostUp and PreDown rules that reject anything not leaving through the tunnel,
  because `wg-quick` has no kill switch by default. That also blocks the local network and incoming sessions such as SSH.
  It needs `wireguard-tools`, `iptables`, and `ip6tables`.
* The Linux client has been tested on Ubuntu 24.04 only. The Windows client has not been tested on a real machine, so
  its kill switch is not trusted yet.

## Enrollment

Enrollment is built and works against a real hub on AWS.

* An invite is one line, `niblr1:<base64url JSON>`. It holds the enrollment address, the hub's WireGuard endpoint and
  public key, a sha256 pin of the hub's enrollment certificate, a one-time token, and an expiry. It is a bearer secret
  until used. It is created on the hub with `niblrd invite --name alice --hub-ip IP`. A remote `niblr invite` could wrap
  this later.
* `niblr join <invite>` generates the tester's WireGuard key on their machine, so the private key never leaves it. It
  then sends only the public key and token to `https://hub:8443/v1/enroll` over TLS 1.3, pinned to the certificate
  fingerprint in the invite (no CA, no hostname, nothing to misconfigure). The hub replies with the tester's address
  (`10.10.9.N`), the hub key, the endpoint, DNS, and MTU, and the client checks the hub key against the invite. Before it
  uses the invite, the client checks for administrator rights and for WireGuard, so those failures do not use up the
  one-time token.
* An early design enrolled over WireGuard instead, with an invite that was a one-time key. The built design uses pinned
  TLS on TCP 8443, so the hub exposes that port as well as UDP.
* On the hub, tokens are stored only as sha256 hashes and are single use and time limited. A lost reply to the same
  machine is answered identically, and a different machine presenting a used token is refused. A failed peer-add does
  not use up the token. Failed attempts are limited to 10 per minute globally, so a guesser can briefly lock out real
  enrollments, a trade-off accepted over allowing unlimited guesses. State is `enroll.json` (mode 0600) under a file
  lock, re-read on every operation so `niblrd invite` and the daemon never overwrite each other. Enrolled testers are
  re-added to the interface at every daemon start.
* IPv6 goes into the tunnel and ends at the hub, which has no IPv6 route. This has been verified in configuration only,
  because the test VPC has no IPv6.
* Known limits: there is no revoke command (a tester can be removed with `wg set wg-op peer <pub> remove` and by editing
  `enroll.json`), and invites cannot be created from the admin's workstation yet.

## Status and order of work

Built and tested on AWS: the data plane, the managed exit pool, rotation, enrollment, and the Linux client. Still to do
on AWS: a revoke command, creating invites from the admin's workstation, measuring IPv6 behavior, testing on a real Windows machine, load testing near the conntrack limit, and removing
the legacy Python, Terraform, and Ansible files.

## Roadmap

This is planned and nothing here is built. The target test matrix is layer-3 exits in AWS, Azure, and GCP, and
HTTPS and SOCKS5 workers in AWS Lambda and GCP serverless. Items marked *(verify)* are unconfirmed platform claims.
Check them against current documentation and a real account before building on them.

### More exit clouds

Each new cloud is a new `Provider` implementation on the same data plane.

* One hub can use exits in any cloud, because exits already connect to the hub over the internet. Mixed-cloud pools
  also draw source addresses from several networks.
* On GCP, `instances.insert` takes a request ID (idempotent) and labels at creation. A shutdown from inside leaves the
  VM `TERMINATED`, which still bills for its disks, so use the "broken" state with reconciler deletion, or a termination
  action of DELETE where available *(verify)*. Each VM gets an ephemeral external IP, and `hub-ip` exits do not fit.
* On Azure, PUT is idempotent by resource name, so derive the VM, network interface, and public IP names from the token.
  A stopped VM keeps billing unless it is deallocated, so the lease must deallocate or delete instead of only shutting
  down *(verify)*. A VM is several resources (VM, network interface, public IP, and disk), so tag them all at creation
  and delete them together.
* Open points are slot ownership across providers and per-cloud permission scoping. Slots are conntrack marks, so they
  must be globally unique. Assign each slot to a provider deterministically and let each provider's reconciler see only
  its own slots. Each cloud also needs scoping equivalent to [IAM-aws.md](IAM-aws.md).

### Workers: HTTPS and SOCKS5 without a VM

Workers have a different shape from exits. They use no WireGuard and no slot, and the hub does not carry their traffic
as a tunnel. They need a new interface (`WorkerProvider`, with deploy, list, and delete, and the same creation-time tags
and lease rules) instead of overloading `Provider`.

* On GCP, Cloud Run or functions gen2 support HTTP/2 and WebSockets *(verify)*, so a worker can be a real TCP relay, from
  a SOCKS5 listener through a WebSocket to the worker and on to the target. That gives SOCKS5 and CONNECT behavior for
  TCP only, with no UDP or ICMP. Egress comes from Google's shared pool by default, and a stable egress address needs
  VPC egress and Cloud NAT *(verify)*.
* AWS Lambda cannot accept raw TCP, and a Function URL is plain HTTPS request and response. A Lambda worker would
  therefore be an HTTP(S) fetch relay, where the worker makes the request for the client, and not a SOCKS5 server or a
  CONNECT tunnel *(verify; response streaming may help with large bodies)*. Payload and duration limits apply, and source
  addresses come from AWS's shared Lambda pool and cannot be controlled. It is cheap and rotates addresses, but it
  supports only HTTP(S) testing, and only where the client allows TLS termination at the worker, so the tester's tool
  sees the worker's TLS instead of the target's.

#### Workers are private, never a public proxy

The hub fronts the workers, and testers never hold cloud credentials.

* A tester joins in a proxy-only mode. The invite and enrollment are the same, but the WireGuard config routes only the
  hub's tunnel address (`10.10.9.1`), not all traffic. The hub runs a SOCKS5 and HTTP listener bound only to that
  address, so reaching it takes a valid WireGuard key. That is the same trust model as the layer-3 path, and nothing is
  open to the internet.
* For each new connection, the hub picks a healthy worker at random and calls it with its own cloud identity.
* Workers reject anonymous callers by construction. Lambda is invoked through the Invoke API (no URL at all), or through
  a Function URL with `AuthType=AWS_IAM` if a URL is ever needed. Cloud Run and functions are deployed with
  unauthenticated invocation disallowed, so each call needs a Google-signed token that the hub's service identity mints.
* As a second layer, the hub signs each request with a short-lived token the worker verifies, and workers refuse
  private, RFC1918, and metadata destinations. A leaked cloud credential alone is then not enough.
* The cost is that proxy mode lets a tool that ignores its proxy setting exit from the tester's own address. Full
  layer-3 mode has no such leak and stays the default. For Lambda, HTTPS targets would need the hub to terminate TLS, so
  testers would trust a hub certificate authority, as with Burp. Cloud Run's TCP relay keeps TLS end to end *(verify
  both)*.
* Pool health works the same way. The hub probes each worker end to end by fetching a known URL through it and drops
  failures from the pool. With no healthy worker, the hub's proxy refuses connections and never connects directly.

### Cross-cloud credentials

This is the planned direction, and nothing in it has been tested. All platform claims are *(verify)*.

The hub should never hold long-lived keys for another cloud. It proves its own identity, which is its AWS instance
role, and trades that for short-lived credentials in the other cloud. The direction matters.

* From the hub on AWS to GCP (to create GCP exits or call Cloud Run workers), use GCP Workload Identity Federation. A
  workload identity pool trusts the hub's AWS role, and the hub exchanges a signed AWS identity for a short-lived Google
  token scoped to a service account that has only the roles NiBLR needs. This also lets Cloud Run keep unauthenticated
  invocation disallowed.
* From the hub on AWS to Azure, use a federated credential on a managed identity or app registration, with the hub's
  identity as the issuer. This is more awkward with an AWS issuer than GCP's native AWS support.
* From outside AWS into AWS, use AWS IAM Roles Anywhere, which turns an X.509 certificate into AWS credentials. It is
  needed only if the hub or an exit runs outside AWS and must call Lambda or EC2.
* Setup is a one-time step per cloud, done by someone with administrator rights there, with `niblr init` using the
  admin's own credentials. Each cloud gets a least-privilege service account or identity, equivalent to
  [IAM-aws.md](IAM-aws.md).
* If federation proves impractical for a cloud, the fallback is a narrowly scoped key stored with mode 0600 on the hub.
  Workers then rely on the signed per-request token and destination block list, which leaves a reachable endpoint that
  is gated by a token.

### Suggested order

1. A GCP exit provider. It is the smallest change and proves GCP Workload Identity Federation and provider-owned slots.
2. An Azure exit provider.
3. The worker protocol, hub-side proxy listener and proxy-only join, and the GCP Cloud Run TCP-over-WebSocket worker.
4. The Lambda HTTP(S) fetch worker, with its limits documented.

Each step needs an account and credentials for that cloud.

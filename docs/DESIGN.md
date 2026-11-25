# NiBLR design

Goal: an operator runs `niblr join`, gets a full L3 connection, and their traffic leaves from
rotating public addresses. Operators are not network people: nothing to configure, plain-language
errors, no proxy settings.

## Shape

Go, one module, direct cloud SDKs. No Terraform, no Pulumi, no Ansible.

* `cmd/niblr`  - the operator/admin CLI (`join`, `status`, `rotate`, `scale`, `down`).
* `cmd/niblrd` - the hub daemon (health checks, exit pool, rotation, operator enrollment API).
* `internal/provider` - cloud contract. One small implementation per cloud.
* `internal/pool`, `internal/dataplane`, `internal/health` - the hub's brains (pure logic is unit-tested without root).

Why not Terraform/Pulumi: operators would need them installed, rotation is imperative churn
(create / replace / re-IP in a loop), and `join` needs idempotent discovery. State lives in cloud tags
plus the hub; there is no state file to lose.

## Data plane (proven locally and on AWS, see prototype/)

* Operator = WireGuard peer on the hub with its own /32 and key.
* Each exit has its own WireGuard interface on the hub. Exits DIAL the hub (so their public IP can change).
* nftables marks each NEW operator flow with an exit id stored in conntrack; `ip rule fwmark` selects the
  exit's route table. Every packet of a flow, both directions, uses the same exit (symmetric by construction).
* A health loop rewrites the marking rules atomically. No healthy exit => `drop` (fail closed).
* Health = recent handshake AND an end-to-end probe (marked ping sourced from the operator subnet, out
  via the exit and NAT to the internet and back). A tunnel-only probe is NOT enough: it kept routing into an
  exit that had lost its return route.
* Draining an exit (leave pool, keep tunnel) never breaks established flows.

## Exit types (same pool, same health, same marking)

| type | what | L3 | notes |
|---|---|---|---|
| vm | small VM per exit, dials the hub | yes | any provider |
| hub-ip | extra private IPs on the hub mapped 1:1 to public IPs, chosen by `nft snat` | yes | one box, N addresses; untested |
| proxy | container / function exits | TCP via SOCKS or HTTP only | later; optional |

## Operator flow

1. Admin: `niblr init --env X` creates the hub with a scoped cloud role (cloud creds needed only here).
2. Operator: `niblr join <invite>` registers their WireGuard key with the hub, gets a config, brings the tunnel up.
3. `niblr status` says in plain language which address they are leaving from and when it next changes.

If `join` is run with cloud credentials and no environment exists, it creates one first (init + join).

## Provider contract (sketch)

    type Provider interface {
        Name() string
        CreateExit(ctx, spec ExitSpec) (Exit, error)   // boots with the exit cloud-init, dials the hub
        ReplaceExit(ctx, id string) (Exit, error)      // new public IP, same slot/key
        DeleteExit(ctx, id string) error
        ListExits(ctx, env string) ([]Exit, error)     // discovery by tag, no state file
    }

## Known operational notes

* wg-quick@ units hit systemd start limits when restarted >5x in 10s: exit units set Restart=on-failure and a relaxed limit.
* Per-flow rotation can break targets that pin sessions by source IP: rotation policy is configurable.

## Review findings adopted (Opus design review, checked against what the prototype showed)

**Lifecycle (must exist before rotation/scale):**
* Atomic tag-on-create (`TagSpecifications`) and idempotent creates (`ClientToken`), so a half-finished create is never an untagged, billed orphan.
* A GC loop on the hub, plus an out-of-band `niblr down` that sweeps by environment tag without needing the hub.
* Leases so a dead hub cannot leave exits running (instance-initiated shutdown / on-box dead-man; GCP `maxRunDuration`).
* Never reuse an exit mark id without `conntrack -D --mark N` first. Ordering invariant: route + interface up BEFORE an id enters the map; remove the id BEFORE teardown.

**Control-plane security (the hub is internet-facing):**
* Operators enroll OVER WireGuard: an invite is a one-time WG key whose peer can reach only the enrollment API on the hub's tunnel IP. The hub exposes only silent UDP. This settles the enrollment transport.
* The hub's cloud role is scoped to the minimum: no `iam:PassRole` (exits get no instance profile), `ec2:CreateTags` only with `ec2:CreateAction=RunInstances`, IMDSv2 required. A dedicated account/SCP and a budget alarm are the only hard ceiling.
* `vm` exits use ephemeral public IPs (they dial out). EIPs are only for the hub endpoint (and the hub-ip type).
* The hub drops link-local/metadata (169.254.169.254, 168.63.129.16), RFC1918 and CGNAT destinations in the mark chain so operators cannot pivot into the infrastructure.

**Operator client (first-class, not an afterthought):**
* AllowedIPs `0.0.0.0/0, ::/0` (IPv6 would otherwise leak the real address), DNS through the tunnel, hub drops v6 it cannot carry.
* Use the official WireGuard client's kill-switch instead of a hand-rolled dead-man (ours failed OPEN). Target OSes: decide early; emitting a config for the WireGuard app is the cheap v1.

**Data plane traps to handle:**
* conntrack exhaustion under scanning: raise `nf_conntrack_max`/hashsize, tune timeouts, alert on table fill.
* MTU/MSS: tunnels are chained at the hub (inner ~1420); set per-provider MTUs (GCP 1460 default) and clamp MSS on the hub. Add a large-payload test.
* Pin `rp_filter` per interface after creation.
* Hub is cattle, not HA: config derivable from tags, hub key in a secret store, stable endpoint (EIP/DNS), auto-restart. One hub per engagement limits blast radius. Do not size it as a `t3.nano` beyond tests (burst network credits).

**hub-ip exit type:** gated per provider. AWS and Azure support many IPs per NIC (size-limited, needs `ip addr add` by niblrd); GCP allows one external IP per NIC, so it does not fit. `vm` is the only portable type.

## Build order (revised)

1. Thin vertical slice first: minimal AWS provider (create/list/delete with atomic tags + GC), minimal CLI (`join`, `status`), real niblrd. Prove it reproduces the AWS prototype, including large payload/MTU and conntrack under load, plus a rooted netns integration test.
2. Then rotate/scale/replace (churn, quotas, slot reuse), then the operator client leak-proofing.
3. Stub the second provider's signatures during step 1 to catch an AWS-shaped interface early.
4. Delete the legacy Python/Terraform/Ansible once the AWS slice is proven.

## Shared AWS account (decided: the account is shared, not dedicated)

See `docs/IAM-aws.md`. The hub may only create inside NiBLR's own VPC/subnet/SG and only touch instances tagged
`niblr-managed=true` (and `niblr-env=<its env>`). The reconciler must NEVER list-then-delete anything that lacks
those tags, even if it looks like a stray. Hard caps (`max-exits`, budget alarm) protect shared quota and spend.
Do not use the default VPC. The IAM policy is a sketch and has not been validated against a real account.

## Operator client (Linux and Windows operators)

`niblr join` writes a WireGuard config and brings the tunnel up using the official client on each OS.
Config always routes `0.0.0.0/0, ::/0` and sets `DNS=` to the hub, so nothing leaks around the tunnel.

* Windows: WireGuard for Windows; `niblr` installs the tunnel service (`wireguard.exe /installtunnelservice`,
  needs an admin prompt). With `0.0.0.0/0, ::/0` the app's "block untunneled traffic" kill-switch is active.
* Linux: `wg-quick` with PostUp/PreDown rules that REJECT anything not leaving through the tunnel (wg-quick has no
  kill-switch by default), plus IPv6 and DNS. Needs `wireguard-tools`.
* Neither is tested yet. Windows in particular needs a real run before the kill-switch is trusted.

## Open

* Invite token format (one-time WG key + enrollment API), and hub credential story for non-home clouds (static tokens on the hub: scope tightly).
* Second provider (DigitalOcean planned).

## Operator enrollment (built; unit-tested, not yet run on a real hub)

* **Invite** = one line, `niblr1:<base64url JSON>`: enrollment address, hub WireGuard endpoint, hub WireGuard public
  key, sha256 pin of the hub's enrollment certificate, a one-time token, an expiry. It is a bearer secret until used.
  Minted on the hub with `niblrd invite --name alice --hub-ip IP` (a remote `niblr invite` can wrap this later).
* **`niblr join <invite>`** generates the operator's WireGuard key *on their machine* (the private key never leaves
  it), then POSTs only the public key and token to `https://hub:8443/v1/enroll` over TLS 1.3 **pinned to the
  certificate fingerprint in the invite** (no CA, no hostname, nothing to misconfigure). The hub replies with the
  operator's address (10.10.9.N), the hub key, endpoint, DNS and MTU; the client checks the hub key against the invite.
* **Hub side:** tokens are stored only as sha256 hashes, are single-use and expire; a lost reply from the same machine
  is answered identically (idempotent), a different machine presenting a used token is refused; a failed peer-add does
  not burn the token; failed attempts are rate-limited (10/min globally, so a guesser can briefly lock out real
  enrollments, which was judged better than allowing guessing). State is `enroll.json` (0600) under a file lock, re-read
  on every operation so `niblrd invite` and the daemon never overwrite each other. Enrolled operators are re-added to
  the interface at every daemon start.
* **Client leak-proofing:** AllowedIPs `0.0.0.0/0, ::/0` (IPv6 goes into the tunnel and dies at the hub, which has no
  IPv6 route: fail closed), DNS through the tunnel, MTU 1420. Linux: wg-quick plus PostUp/PreDown iptables+ip6tables
  REJECT of anything not using the tunnel. Windows: relies on WireGuard for Windows's built-in kill switch
  (active when AllowedIPs covers both families). **None of the leak behaviour has been tested on a real machine yet.**
* **Known limits:** the hub is cattle, so a rebuilt hub means re-inviting operators (its enrollment state lives on its
  disk); there is no revoke command yet (an operator can be removed with `wg set wg-op peer <pub> remove` and by editing
  `enroll.json`); invites cannot be minted from the admin's workstation yet.

## Roadmap: beyond one cloud, beyond L3 (direction from the project owner; nothing here is built yet)

Target test matrix: **L3 nodes in AWS, Azure and GCP; HTTPS/SOCKS5 workers in AWS Lambda and GCP serverless.**
Items marked *(verify)* are my understanding of the platforms and must be checked against current docs and a real
account before anything is built on them.

### More node clouds (same data plane, new `Provider` implementations)
* One hub, exits in any cloud: exits already dial the hub over the internet, so the hub does not care where they live.
  Mixed-cloud pools also diversify the source networks, which suits the "look like several users" goal.
* **GCP:** instances.insert takes a request id (idempotent) and labels at create time. A shutdown from inside leaves the
  VM `TERMINATED` (still exists, still bills disks), so use the "broken" state plus reconciler deletion, or a
  termination action of DELETE where available *(verify)*. Ephemeral external IP per VM. `hub-ip` exits do not fit GCP.
* **Azure:** PUT is idempotent by resource name, so derive the VM/NIC/public-IP names from the token. A stopped VM keeps
  billing unless *deallocated*, so the lease must deallocate/delete rather than just shut down *(verify)*. A VM is
  several resources (VM, NIC, public IP, disk): tag all at create and delete them together.
* **Open design points:** (a) hub credentials for a *second* cloud: workload-identity federation from the hub's AWS role
  is preferable to long-lived keys *(verify feasibility)*; (b) slot ownership across providers: slots must be globally
  unique (they are conntrack marks), so assign each slot to a provider deterministically and let each provider's
  reconciler see only its own slots; (c) per-cloud IAM scoping equivalent to `docs/IAM-aws.md`.

### Workers: HTTPS / SOCKS5 without a VM
Workers are a different shape from nodes: no WireGuard, no slot, no hub in the data path. A new interface
(`WorkerProvider`: deploy / list / delete, same create-time tags/labels and lease rules) rather than overloading `Provider`.
The operator runs a local SOCKS5/HTTP listener (`niblr proxy`, on 127.0.0.1) which picks a worker per new connection
(random over the healthy set, exactly the same rotation idea) and forwards over an authenticated HTTPS transport.

* **GCP (Cloud Run / functions gen2):** supports HTTP/2 and WebSockets *(verify)*, so a worker can be a real TCP relay:
  local SOCKS5 -> WebSocket -> worker -> target. That gives genuine SOCKS5/CONNECT semantics (TCP only, no UDP/ICMP).
  Egress is Google's shared pool by default; a stable egress address needs VPC egress plus Cloud NAT *(verify)*.
* **AWS Lambda:** cannot accept raw TCP, and a Function URL is plain HTTPS request/response, so a Lambda worker is an
  **HTTP(S) fetch relay** (the worker makes the request on the client's behalf), not a SOCKS5 server and not a CONNECT
  tunnel *(verify; response streaming may help with large bodies)*. Payload/duration limits apply. Source addresses come
  from AWS's shared Lambda pool and are not controllable. Cheap and rotating, but only for HTTP(S) testing, and only
  where the client lets us terminate TLS at the worker (so the operator's tool sees the worker's TLS, not the target's).
  Be explicit about this limit in the operator docs.
* **Security (critical):** a public function that proxies is an open relay. Every worker must require a signed,
  short-lived token (minted from the same invite/enrollment system) and refuse private/metadata destinations, as the hub
  does. Workers are scoped, tagged and leased like VMs.
* **Pool health:** probe each worker end to end (fetch a known URL through it); drop failures from the pool; same
  fail-closed rule (no healthy worker means the local proxy refuses connections, never connecting directly).

### Suggested order
1. GCP node provider (smallest delta; proves the second-cloud credential story and provider-owned slots).
2. Azure node provider.
3. Worker protocol, local `niblr proxy`, and the GCP Cloud Run TCP-over-WebSocket worker.
4. Lambda HTTP(S) fetch worker, with its limits documented.
Each step needs credentials/accounts for that cloud, which I do not have yet; I will ask before creating anything.

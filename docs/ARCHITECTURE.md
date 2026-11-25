# How NiBLR works

This page explains how NiBLR works today. [DESIGN.md](DESIGN.md) records the reasoning and the roadmap.

## The picture

    tester ──WireGuard──▶ hub ──one WireGuard tunnel per exit──▶ exit VM ──▶ internet
                            ▲                                       │
                            └───────── exits connect to the hub ────┘

* A tester is a WireGuard peer (a client) on the network `10.10.9.0/24`. All of the tester's traffic goes to the hub.
* An exit is a small cloud VM. Each exit connects to the hub, so it needs no inbound ports. It applies network address
  translation (NAT), so outbound traffic leaves from the exit's own public address.
* The hub has one WireGuard interface per exit (`wg-eN`) and one for testers (`wg-op`, where `op` is short for operator, the project's older word for a tester).

## Choosing an exit

On the hub, nftables gives every new tester connection a firewall mark (fwmark) that is the exit number, chosen at
random from the healthy exits. The mark is saved with the connection (in conntrack, the kernel's connection table) and
applied again to the connection's later packets, so a connection never changes exit midway. An `ip rule` matching
`fwmark N` sends marked traffic to routing table `1000+N`, where N is the exit number, and that table's default route
is the exit's tunnel. Replies return through the same tunnel, so each connection uses one exit in both directions.

### Blocked destinations

Before an exit is chosen, the hub drops new connections to private, link-local, and cloud metadata addresses (for example
`10.0.0.0/8` and `169.254.169.254`). Without this, a tester could use an exit to reach the network around it.

### Failing closed

A fallback routing rule for tester traffic ends in an unreachable route. When no exit is healthy, new connections are
dropped, and no traffic leaves from the hub's own address.

## Health

An exit stays in the pool only while both conditions hold. Its last WireGuard handshake is under 200 seconds old, and a
ping to `1.1.1.1` sent through it succeeds. A drained exit leaves the pool while its tunnel stays up. The hub applies
pool changes atomically with a single `nft -f` call.

## The reconcile loop

The hub keeps no state file for exits. It reads their state from cloud tags. The daemon runs this loop every 15 seconds
by default (`--reconcile`).

1. List the exits in the cloud by tag (`niblr-managed`, `niblr-env`, `niblr-slot`, and the exit's public key). A slot is
   a numbered position in the pool.
2. Plan the changes against `--want`. Create missing slots, and delete strays (instances with an unusable slot),
   duplicates, and stopped instances, which NiBLR treats as broken.
3. Apply the plan through the provider (AWS today). Each create call carries an idempotency token, so a retried create
   cannot launch a duplicate, and sets its tags at creation time.
4. Sync the hub with what exists. Build interfaces for new exits and remove interfaces for exits that are gone.

The hub removes a slot from the routing pool before tearing down its interface, then purges that slot's conntrack
entries.

To rotate an exit, delete its instance. The loop refills the slot with a new instance that has a new address and key.
If the hub is lost, a new hub rebuilds the exit list from the cloud tags. Two things do not survive a lost hub. Each
exit has the hub's public IP built in, so the new hub needs the same address. The hub also keeps the list of enrolled
testers on its own disk, so testers need new invites.

## Joining

`niblr join` generates the tester's key locally and sends only the public key and the invite token to the hub. The
connection uses TLS pinned to the certificate fingerprint embedded in the invite. The hub adds the peer and returns the
tunnel settings.

The client configuration routes all IPv4 and IPv6 traffic (`0.0.0.0/0` and `::/0`) into the tunnel and forces DNS through
it. The hub has no IPv6 route, so IPv6 traffic ends there. On Linux, a firewall rule also blocks any traffic outside the
tunnel.

## Code map

    cmd/niblr           the tester and admin command-line tool
    cmd/niblrd          the hub daemon (static mode, managed mode, and the invite subcommand)
    internal/provider   the cloud interface (CreateExit, DeleteExit, ListExits), with aws/ and fake/
    internal/reconcile  planning and applying changes, level-triggered
    internal/hub        the Manager (syncs the hub to the exits) and Base (one-time hub setup)
    internal/boot       exit cloud-init and the exact hub commands
    internal/pool       the exit pool and its health state
    internal/dataplane  the nftables ruleset
    internal/health     health checks
    internal/enroll     invites, and the enrollment server and client
    internal/client     renders and applies the tester's tunnel configuration (Linux and Windows)

In static mode, `niblrd` is given a fixed list of exits with `--exits`. In managed mode, it creates and replaces exits
itself.

## Not yet implemented

NiBLR does not yet support other clouds, SOCKS5 or HTTPS workers, IPv6 exits, or revoking a tester. See the roadmap in
the README.

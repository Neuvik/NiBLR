# NiBLR operator guide

For testers. No networking knowledge needed. If something here doesn't work, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## What you get

While you are connected, **all** of your computer's internet traffic goes through NiBLR and comes out of
rotating public addresses. Each *new* connection (a new page load, a new scan connection, a new login attempt)
may leave from a different address. One existing connection keeps the same address until it ends, so a login
session does not break halfway through.

## Before you start

You need three things:

1. The `niblr` program for your computer (ask your admin where to get it).
2. An **invite** from your admin. It looks like `niblr1:` followed by a long string. It works **once** and expires.
3. Administrator rights on your computer (to set up the network connection).

Only use NiBLR on engagements you are authorized for. See [ACCEPTABLE-USE.md](ACCEPTABLE-USE.md).

## Linux

1. Install WireGuard if it isn't installed: `sudo apt install wireguard` (or your distribution's equivalent).
2. Connect:

       sudo niblr join niblr1:PASTE-YOUR-INVITE-HERE

3. You should see: *"Joined. All of your internet traffic now goes through NiBLR..."*
4. Check it:

       sudo niblr status

   A good answer looks like *"Connected. Websites currently see you as: 203.0.113.10"*. Run it a few times; a
   working setup shows different addresses over time.
5. Disconnect when you're done:

       sudo niblr leave

## Windows

1. Install **WireGuard for Windows** from <https://www.wireguard.com/install/>. You don't need to open it or set anything up.
2. Open **PowerShell as Administrator** (right-click PowerShell, "Run as administrator").
3. Connect:

       niblr.exe join niblr1:PASTE-YOUR-INVITE-HERE

4. Check with `niblr.exe status` and disconnect with `niblr.exe leave`.

> Windows support builds and is designed to block traffic if the tunnel drops, but it has **not yet been tested on a
> real Windows machine**. Until it has, confirm your address with `niblr.exe status` before starting real work.

## Is it working?

* `niblr status` says **Connected** and lists addresses: yes.
* It says **Joined, but the internet isn't reachable...**: you are safe (traffic is blocked, not leaking), but nothing
  loads. Wait a minute and try again; if it stays like this, tell your admin.
* It says **Not joined**: you are on your normal connection, not through NiBLR.

You can also visit an "what is my IP" site. You should **not** see your own address.

## Rotating your address

You don't need to do anything. New connections pick a random available exit. If you need a fresh address *right now*
(for example after a block), start a new connection (a new browser tab, or re-run your tool). If a particular
address is burned, ask your admin to replace that exit; it comes back with a new address within about a minute.

## What NiBLR protects, and what it doesn't

* Your real address is **not** used while joined: DNS goes through the tunnel, IPv6 is sent into the tunnel and
  dropped there, and on Linux a firewall rule blocks anything that tries to bypass the tunnel.
* It does **not** hide who *you* are to the sites you log in to. Logging in with your own account identifies you.
* Your traffic is visible to NiBLR's admin and to the cloud exit it passes through. Treat it like any VPN you don't own.
* The tunnel is a little slower than your normal connection (two encrypted hops, roughly 1400-byte packets).

## Finishing

Run `niblr leave`. It removes the connection and the key, and your internet goes back to normal. To join again you
need a **new** invite.

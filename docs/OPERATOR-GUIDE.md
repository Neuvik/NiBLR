# NiBLR tester guide

This guide is for penetration testers. You do not need to know anything about networking. If something goes wrong,
see [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## What you get

While you are joined, all of your computer's internet traffic goes through NiBLR and leaves from one of several public
addresses. Each new connection (a page load, a scan connection, a login attempt) may use a different address. A
connection that is already open keeps its address until it ends. A browser or tool often opens several connections for
one session, so a site that ties a login to a single source address may log you out. There is no way to pin a target to
one exit yet.

Three words you will see:

* The hub is the NiBLR server your admin runs.
* An exit is a small cloud machine that sends your traffic on to the internet. Each exit has its own public address.
* The tunnel is the encrypted link between your computer and the hub.

## Before you start

You need three things.

1. The `niblr` program for your computer. Your admin will tell you where to download it.
2. An invite from your admin. It starts with `niblr1:`. It works once and then expires.
3. Administrator rights on your computer (`sudo` on Linux, an Administrator PowerShell window on Windows). NiBLR needs
   them to change your network settings.

Only use NiBLR on engagements you are authorized for. See [ACCEPTABLE-USE.md](ACCEPTABLE-USE.md).

`niblr join` checks for administrator rights and for WireGuard before it uses your invite, so those mistakes do not use
it up. A failure after the message "Connecting to NiBLR..." can, and then you need a new invite.

## Linux

1. Install WireGuard, the VPN software NiBLR uses, along with the firewall tool `iptables`. On Debian, Ubuntu, and
   Kali, run `sudo apt install wireguard iptables resolvconf`. On other distributions, install the packages that
   provide `wg-quick`, `iptables`, and `ip6tables`, and a way for `wg-quick` to set DNS (`resolvconf` or
   `systemd-resolved`). NiBLR has been tested on Ubuntu 24.04 only.
2. Run `command -v wg-quick`. If it prints a path, you are ready.
3. Do not run `join` over an SSH session or from a remote desktop. The firewall rule stops your computer from answering
   anything outside the tunnel, so an SSH or remote desktop session will drop.
4. Make the downloaded program runnable, and run it from the folder that holds it. For example, if it is in your
   Downloads folder:

       cd ~/Downloads && chmod +x niblr

   Use `./niblr` where the commands below say `niblr`, or move it to `/usr/local/bin` with
   `sudo mv niblr /usr/local/bin/` so that plain `niblr` works.
5. Join:

       sudo ./niblr join niblr1:PASTE-YOUR-INVITE-HERE

   The program prints "Connecting to NiBLR..." and then a message that starts with "Joined."
6. Check the connection. Use `sudo` here too, because without it `niblr status` cannot see the connection and says
   "Not joined" even when you are joined.

       sudo ./niblr status

   A good answer looks like "Connected. Websites currently see you as: 203.0.113.10, 198.51.100.7". `status` opens a
   few separate connections, so you can see more than one address.
7. Leave when you are done:

       sudo ./niblr leave

## Windows

1. Install WireGuard for Windows from <https://www.wireguard.com/install/>. You do not need to open it or set anything
   up.
2. Click Start, type PowerShell, right-click Windows PowerShell, and choose Run as administrator.
3. In PowerShell, go to the folder that holds `niblr.exe`. For example, if it is in your Downloads folder:

       cd $HOME\Downloads

4. Join. Windows may warn that the program is from an unknown publisher, because it is not signed. Continue only if
   your admin gave it to you.

       .\niblr.exe join niblr1:PASTE-YOUR-INVITE-HERE

5. Check the connection:

       .\niblr.exe status

6. Leave when you are done:

       .\niblr.exe leave

Windows support has not been tested on a real Windows machine. It is designed to block traffic if the tunnel drops, but
that is unconfirmed. Before real work, check your address with `niblr.exe status` and a "what is my IP" website. If
`niblr` says WireGuard is not installed when it is, tell your admin.

## Is it working?

If `niblr status` says "Connected", it is working. A "what is my IP" website should show an address that is not yours.

If it says "Joined, but the internet isn't reachable", NiBLR is blocking your traffic on purpose so your real address
does not leak. Nothing will load. Wait a minute and try again. If it stays this way, tell your admin.

If it says "Not joined", your traffic is not going through NiBLR. You are on your normal connection. On Linux, make sure
you ran it with `sudo`.

## Rotating your address

You do not need to do anything. Each new connection uses one of the available exits at random. To get a different
address right away, open a new connection, such as a new browser tab or a fresh run of your tool. If a client has
blocked one address, ask your admin to replace that exit. The replacement has a new address within about a minute.

## What NiBLR protects, and what it does not

* While you are joined, websites do not see your real address. Name lookups (DNS) go through NiBLR, using Cloudflare
  (`1.1.1.1`) and Quad9 (`9.9.9.9`) from the exit. IPv6, a newer kind of address that NiBLR does not support, is
  blocked. On Linux, a firewall rule blocks any traffic that tries to skip NiBLR. The Windows protection has not been
  tested.
* The Linux firewall rule also stops your computer from reaching devices on your local network, such as printers and
  file shares, while you are joined.
* NiBLR does not hide your identity from sites you log in to. If you sign in with your own account, the site knows it is
  you.
* Your admin and the cloud provider running each exit can see your traffic. Treat NiBLR like any VPN you do not
  control, and do not send anything through it that you would not send through a third-party VPN.
* NiBLR does not yet block your traffic to private and cloud metadata addresses, so do not rely on it to keep you out of
  the network around the exits.
* Connections only go outward. Exits accept nothing from the internet, so reverse shells and callbacks to your computer
  will not work.
* NiBLR is slower than your normal connection because your traffic is encrypted and passes through two servers, the
  hub and then an exit. Expect lower speeds on large downloads and fast scans.

## Finishing

Run `sudo niblr leave` on Linux or `.\niblr.exe leave` in an Administrator PowerShell on Windows. It removes the
connection and your key, and your internet goes back to normal. To join again, you need a new invite.

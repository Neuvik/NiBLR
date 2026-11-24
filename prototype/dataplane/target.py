#!/usr/bin/env python3
"""Fake internet host. TCP 8080: each line you send gets 'src=<ip>' back. UDP 9999 echoes src."""
import socket, threading
def tcp_client(c, addr):
    f = c.makefile("rw")
    for line in f:
        f.write(f"src={addr[0]}\n"); f.flush()
def tcp():
    s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("0.0.0.0", 8080)); s.listen(256)
    while True:
        c, a = s.accept(); threading.Thread(target=tcp_client, args=(c, a), daemon=True).start()
def udp():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.bind(("0.0.0.0", 9999))
    while True:
        d, a = s.recvfrom(1024); s.sendto(f"src={a[0]}".encode(), a)
threading.Thread(target=udp, daemon=True).start()
tcp()

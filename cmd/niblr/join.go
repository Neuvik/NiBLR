package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"niblr/internal/boot"
	"niblr/internal/client"
	"niblr/internal/enroll"
)

func applier() client.Applier {
	return client.Applier{
		GOOS: runtime.GOOS, Dir: configDir(), IsAdmin: isAdmin,
		Run: func(name string, args ...string) (string, error) {
			out, err := exec.Command(name, args...).CombinedOutput()
			return string(out), err
		},
		Has:    func(n string) bool { _, err := exec.LookPath(n); return err == nil },
		Exists: func(p string) bool { _, err := os.Stat(p); return err == nil },
		Write: func(p string, b []byte, m os.FileMode) error {
			if err := os.MkdirAll(dirOf(p), 0o700); err != nil {
				return err
			}
			return os.WriteFile(p, b, m)
		},
		Remove: os.Remove,
	}
}

func dirOf(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return "."
	}
	return p[:i]
}

func join(args []string) {
	if len(args) != 1 {
		fail("Usage: niblr join <invite>\n\nPaste the whole invite line you were given (it starts with \"niblr1:\").")
	}
	inv, err := enroll.DecodeInvite(args[0])
	if err != nil {
		fail("%v", err)
	}
	a := applier()
	if a.Joined() {
		fail("You're already joined. To use a new invite, run \"niblr leave\" first.")
	}
	priv, pub, err := boot.GenKey() // the private key is made here and never leaves this machine
	if err != nil {
		fail("Couldn't create your security key: %v", err)
	}
	fmt.Println("Connecting to NiBLR...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := enroll.Enroll(ctx, inv, pub, time.Now())
	if err != nil {
		fail("%v", err)
	}
	if err := a.Join(client.Params{PrivateKey: priv, Result: res}); err != nil {
		fail("%v", err)
	}
	fmt.Println("Joined. All of your internet traffic now goes through NiBLR and comes out of rotating addresses.")
	fmt.Println("Run \"niblr status\" to check, and \"niblr leave\" when you're done.")
}

func leave() {
	if err := applier().Leave(); err != nil {
		fail("%v", err)
	}
	fmt.Println("You've left NiBLR. Your internet connection is back to normal.")
}

func status() {
	if !applier().Joined() {
		fmt.Println("Not joined. Run \"niblr join <invite>\" to connect.")
		return
	}
	cl := &http.Client{Timeout: 8 * time.Second}
	var seen []string
	for i := 0; i < 3; i++ { // a few separate connections: new connections may leave from different addresses
		r, err := cl.Get("https://checkip.amazonaws.com")
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 64))
		r.Body.Close()
		seen = append(seen, strings.TrimSpace(string(b)))
		cl.CloseIdleConnections()
	}
	if len(seen) == 0 {
		fmt.Println("Joined, but the internet isn't reachable through NiBLR right now. Nothing is leaking: your traffic is simply blocked until it recovers. Try again in a minute; if it persists, tell whoever runs NiBLR.")
		os.Exit(1)
	}
	fmt.Printf("Connected. Websites currently see you as: %s\n", strings.Join(seen, ", "))
}

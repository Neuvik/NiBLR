// niblrd is the hub daemon: it keeps the exit pool's nftables marking rules in sync with exit health.
//
// This is the first slice of the daemon (health + marking). Operator enrollment, rotation policy and
// the exit lifecycle (via providers) come next.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"niblr/internal/boot"
	"niblr/internal/dataplane"
	"niblr/internal/enroll"
	"niblr/internal/health"
	"niblr/internal/hub"
	"niblr/internal/pool"
	nibaws "niblr/internal/provider/aws"
	"niblr/internal/reconcile"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "invite" {
		inviteCmd(os.Args[2:])
		return
	}
	var (
		opIface  = flag.String("operator-iface", "wg-op", "WireGuard interface operators connect to")
		exitsArg = flag.String("exits", "1,2", "comma-separated exit ids; exit N uses interface wg-eN")
		src      = flag.String("probe-src", "10.10.9.1", "hub address inside the operator subnet, used as the probe source")
		target   = flag.String("probe-target", "1.1.1.1", "host that answers ping, probed through each exit")
		interval = flag.Duration("interval", 2*time.Second, "health check interval")
		resync   = flag.Duration("resync", 30*time.Second, "re-apply the ruleset at least this often, to heal drift")
		drain    = flag.String("drain", "", "comma-separated exit ids to leave out of rotation (tunnels stay up)")
		dry      = flag.Bool("dry-run", false, "print the ruleset instead of applying it")

		// Managed mode: niblrd creates, replaces and removes exits itself.
		want      = flag.Int("want", 0, "number of exits to keep (0 = static mode: use --exits)")
		env       = flag.String("env", "default", "environment name (managed mode)")
		region    = flag.String("region", "us-west-1", "AWS region (managed mode)")
		ami       = flag.String("ami", "", "AMI for exits (managed mode)")
		subnet    = flag.String("subnet", "", "subnet id for exits (managed mode)")
		exitSG    = flag.String("exit-sg", "", "security group id for exits (managed mode)")
		profile   = flag.String("exit-profile", "", "optional IAM instance profile name for exits")
		size      = flag.String("size", "t3.nano", "exit instance type")
		hubIP     = flag.String("hub-ip", "", "this hub's public IP, which exits dial (managed mode)")
		keyFile   = flag.String("key-file", "/etc/niblr/hub.key", "hub WireGuard private key file")
		lease     = flag.Int("lease-minutes", 0, "exits shut themselves down after this long (0 = never)")
		reconcile = flag.Duration("reconcile", 15*time.Second, "how often to reconcile exits with the cloud")
		stateDir  = flag.String("state-dir", "/var/lib/niblr", "where enrollment state and the enrollment certificate live")
		enrollOn  = flag.String("enroll-listen", ":8443", "address for the operator enrollment API (managed mode)")
	)
	flag.Parse()

	var exits []pool.Exit
	var err error
	var mgr *hub.Manager
	if *want > 0 {
		mgr, err = newManager(*want, *env, *region, *ami, *subnet, *exitSG, *profile, *size, *hubIP, *keyFile, *lease)
		if err != nil {
			log.Fatalf("niblrd: %v", err)
		}
	} else if exits, err = parseExits(*exitsArg); err != nil {
		log.Fatalf("niblrd: --exits: %v", err)
	}
	drained, err := parseDrain(*drain)
	if err != nil {
		log.Fatalf("niblrd: --drain: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if mgr != nil && !*dry {
		base := hub.Base{R: execRunner{}, KeyFile: *keyFile, OpIface: *opIface, OpPort: 51000, ConnMax: 262144}
		if err := base.EnsureBase(ctx); err != nil {
			log.Fatalf("niblrd: hub setup failed: %v", err)
		}
		if err := startEnrollment(ctx, base, *stateDir, *enrollOn, *hubIP, *keyFile); err != nil {
			log.Fatalf("niblrd: enrollment: %v", err)
		}
	}

	prober := health.NewProber(*src, *target)
	var last []int
	var lastApplied time.Time
	first := true

	var lastReconcile time.Time
	tick := time.NewTicker(*interval)
	defer tick.Stop()
	for {
		if mgr != nil && time.Since(lastReconcile) >= *reconcile {
			lastReconcile = time.Now()
			exits = reconcileOnce(ctx, mgr, *opIface, last, *dry)
		}
		healthy := pool.Evaluate(ctx, exits, prober, drained)
		if first || !pool.Equal(healthy, last) || time.Since(lastApplied) >= *resync {
			rules := dataplane.RenderMarkRules(*opIface, healthy)
			if err := apply(ctx, rules, *dry); err != nil {
				log.Printf("niblrd: applying rules failed (will retry): %v", err)
			} else {
				if first || !pool.Equal(healthy, last) {
					log.Printf("healthy exits: %s", fmtIDs(healthy))
				}
				last, lastApplied, first = healthy, time.Now(), false
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// reconcileOnce converges the cloud, then the hub's interfaces, and returns the slots that may be probed.
// Slots about to be torn down are taken out of the nft map first (ordering invariant), using the last
// known healthy set minus them, before any interface is removed.
func reconcileOnce(ctx context.Context, m *hub.Manager, opIface string, lastHealthy []int, dry bool) []pool.Exit {
	var nonce [6]byte
	rand.Read(nonce[:])
	plan, err := m.Reconcile(ctx, hex.EncodeToString(nonce[:]))
	if err != nil {
		log.Printf("niblrd: reconcile: %v", err)
	}
	if len(plan.Down) > 0 {
		gone := map[int]bool{}
		for _, s := range plan.Down {
			gone[s] = true
		}
		var keep []int
		for _, id := range lastHealthy {
			if !gone[id] {
				keep = append(keep, id)
			}
		}
		if aerr := apply(ctx, dataplane.RenderMarkRules(opIface, keep), dry); aerr != nil {
			log.Printf("niblrd: could not pull %v from the pool; leaving them in place this round: %v", plan.Down, aerr)
			plan.Down = nil // never tear down an interface whose id is still in the map
		} else if err := m.Down(ctx, plan.Down); err != nil {
			log.Printf("niblrd: teardown: %v", err)
		}
	}
	if err := m.Up(ctx, plan.Up); err != nil {
		log.Printf("niblrd: bring-up: %v", err)
	}
	var exits []pool.Exit
	for _, s := range m.Live() {
		exits = append(exits, pool.Exit{ID: s, Iface: boot.Iface(s)})
	}
	return exits
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, argv ...string) (string, error) {
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func newManager(want int, env, region, ami, subnet, exitSG, profile, size, hubIP, keyFile string, lease int) (*hub.Manager, error) {
	if ami == "" || subnet == "" || exitSG == "" || hubIP == "" {
		return nil, fmt.Errorf("managed mode needs --ami, --subnet, --exit-sg and --hub-ip")
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read hub key %s: %w", keyFile, err)
	}
	hubPub, err := boot.PublicKey(string(raw))
	if err != nil {
		return nil, fmt.Errorf("hub key %s: %w", keyFile, err)
	}
	cfg, err := awscfg.LoadDefaultConfig(context.Background(), awscfg.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws credentials: %w", err)
	}
	p, err := nibaws.New(ec2.NewFromConfig(cfg), nibaws.Config{Env: env, AMI: ami, SubnetID: subnet, SecurityGrp: exitSG, DefaultSize: size, InstanceProfile: profile})
	if err != nil {
		return nil, err
	}
	render := func(slot int) (string, map[string]string, error) {
		priv, pub, err := boot.GenKey()
		if err != nil {
			return "", nil, err
		}
		ci, err := boot.ExitCloudInit(boot.ExitParams{Slot: slot, PrivateKey: priv, HubPublic: hubPub, HubEndpoint: hubIP, LeaseMinutes: lease})
		return ci, map[string]string{"pub": pub}, err
	}
	return &hub.Manager{P: p, Env: env, Want: want, R: execRunner{}, KeyFile: keyFile,
		Tmpl: reconcile.Template{Env: env, Region: region, Size: size, Render: render}}, nil
}

func apply(ctx context.Context, rules string, dry bool) error {
	if dry {
		fmt.Print(rules)
		return nil
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

func parseExits(s string) ([]pool.Exit, error) {
	var exits []pool.Exit
	for _, p := range strings.Split(s, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("bad exit id %q (must be a positive integer)", p)
		}
		exits = append(exits, pool.Exit{ID: id, Iface: fmt.Sprintf("wg-e%d", id)})
	}
	return exits, nil
}

func parseDrain(s string) (map[int]bool, error) {
	m := map[int]bool{}
	if strings.TrimSpace(s) == "" {
		return m, nil
	}
	for _, p := range strings.Split(s, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("bad exit id %q", p)
		}
		m[id] = true
	}
	return m, nil
}

func fmtIDs(ids []int) string {
	if len(ids) == 0 {
		return "none"
	}
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.Itoa(id)
	}
	return strings.Join(s, " ")
}

func enrollConfig(hubIP, keyFile string) (enroll.Config, error) {
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return enroll.Config{}, fmt.Errorf("read hub key %s: %w", keyFile, err)
	}
	pub, err := boot.PublicKey(string(raw))
	if err != nil {
		return enroll.Config{}, err
	}
	return enroll.Config{HubKey: pub, WG: fmt.Sprintf("%s:51000", hubIP), DNS: []string{"1.1.1.1", "9.9.9.9"}, MTU: 1420, Prefix: "10.10.9"}, nil
}

// startEnrollment restores known operators onto the interface and serves the enrollment API.
func startEnrollment(ctx context.Context, base hub.Base, dir, listen, hubIP, keyFile string) error {
	cfg, err := enrollConfig(hubIP, keyFile)
	if err != nil {
		return err
	}
	cert, _, err := enroll.SelfSignedCert(dir)
	if err != nil {
		return err
	}
	srv, err := enroll.NewServer(dir, cfg, peerFunc(base.AddOperator))
	if err != nil {
		return err
	}
	if err := srv.Restore(ctx); err != nil {
		log.Printf("niblrd: restoring operators: %v", err)
	}
	hs := &http.Server{Addr: listen, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}}
	ln, err := tls.Listen("tcp", listen, hs.TLSConfig)
	if err != nil {
		return err
	}
	go func() { <-ctx.Done(); hs.Close() }()
	go func() {
		if err := hs.Serve(ln); err != nil && ctx.Err() == nil {
			log.Printf("niblrd: enrollment server stopped: %v", err)
		}
	}()
	log.Printf("enrollment listening on %s", listen)
	return nil
}

type peerFunc func(ctx context.Context, pub, ip string) error

func (f peerFunc) Add(ctx context.Context, pub, ip string) error { return f(ctx, pub, ip) }

// inviteCmd: `niblrd invite --name alice [--ttl 24h]`, run on the hub. Prints the invite to give the operator.
func inviteCmd(args []string) {
	fs := flag.NewFlagSet("invite", flag.ExitOnError)
	name := fs.String("name", "", "who the invite is for")
	ttl := fs.Duration("ttl", 24*time.Hour, "how long the invite stays valid")
	dir := fs.String("state-dir", "/var/lib/niblr", "state directory")
	hubIP := fs.String("hub-ip", "", "this hub's public IP")
	keyFile := fs.String("key-file", "/etc/niblr/hub.key", "hub WireGuard private key file")
	port := fs.Int("enroll-port", 8443, "enrollment port")
	fs.Parse(args)
	if *name == "" || *hubIP == "" {
		fmt.Fprintln(os.Stderr, "usage: niblrd invite --name NAME --hub-ip IP [--ttl 24h]")
		os.Exit(2)
	}
	cfg, err := enrollConfig(*hubIP, *keyFile)
	if err != nil {
		log.Fatalf("niblrd: %v", err)
	}
	_, pin, err := enroll.SelfSignedCert(*dir)
	if err != nil {
		log.Fatalf("niblrd: %v", err)
	}
	srv, err := enroll.NewServer(*dir, cfg, peerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		log.Fatalf("niblrd: %v", err)
	}
	tok, exp, err := srv.Mint(*name, *ttl)
	if err != nil {
		log.Fatalf("niblrd: %v", err)
	}
	s, err := enroll.Invite{Enroll: fmt.Sprintf("%s:%d", *hubIP, *port), WG: cfg.WG, HubKey: cfg.HubKey, Pin: pin, Token: tok, Expires: exp.Unix()}.Encode()
	if err != nil {
		log.Fatalf("niblrd: %v", err)
	}
	fmt.Printf("Invite for %s (valid until %s, works once):\n\n%s\n\nGive them this, then they run:  niblr join <that line>\n", *name, exp.Format(time.RFC1123), s)
}

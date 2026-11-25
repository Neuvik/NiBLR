// niblrd is the hub daemon: it keeps the exit pool's nftables marking rules in sync with exit health.
//
// This is the first slice of the daemon (health + marking). Operator enrollment, rotation policy and
// the exit lifecycle (via providers) come next.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
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
	"niblr/internal/health"
	"niblr/internal/hub"
	"niblr/internal/pool"
	nibaws "niblr/internal/provider/aws"
	"niblr/internal/reconcile"
)

func main() {
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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"niblr/internal/boot"
	"niblr/internal/provider"
	nibaws "niblr/internal/provider/aws"
)

// dev holds plumbing commands used to drive and test the pieces before `join` exists.
func dev(args []string) {
	if len(args) == 0 {
		fail("usage: niblr dev genkey|hubcmd|exit-create|exit-list|exit-delete")
	}
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	env := fs.String("env", "dev", "environment")
	region := fs.String("region", "us-west-1", "AWS region")
	slot := fs.Int("slot", 0, "exit slot")
	pub := fs.String("pub", "", "peer public key")
	priv := fs.String("priv", "", "exit private key")
	hubPub := fs.String("hub-pub", "", "hub public key")
	hubIP := fs.String("hub-ip", "", "hub public IP")
	keyfile := fs.String("keyfile", "/etc/niblr/hub.key", "hub private key file")
	ami := fs.String("ami", "", "AMI id")
	profile := fs.String("profile", "", "IAM instance profile for exits")
	lease := fs.Int("lease", 0, "exit lease minutes")
	id := fs.String("id", "", "instance id")
	if err := fs.Parse(args[1:]); err != nil {
		os.Exit(2)
	}

	switch args[0] {
	case "genkey":
		a, b, err := boot.GenKey()
		check(err)
		fmt.Println(a, b)
	case "hubcmd":
		if len(args) < 2 {
			fail("hubcmd needs up|down")
		}
		if err := fs.Parse(args[2:]); err != nil {
			os.Exit(2)
		}
		var cs []boot.Cmd
		var err error
		if args[1] == "up" {
			cs, err = boot.HubExitUp(*slot, *pub, *keyfile)
		} else {
			cs, err = boot.HubExitDown(*slot)
		}
		check(err)
		for _, c := range cs {
			fmt.Println(strings.Join(c, " "))
		}
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(*region))
		check(err)
		api := ec2.NewFromConfig(cfg)
		n, err := nibaws.EnsureNetwork(ctx, api, *env, *region+"a")
		check(err)
		p, err := nibaws.New(api, nibaws.Config{Env: *env, AMI: *ami, SubnetID: n.SubnetID, SecurityGrp: n.ExitSG, InstanceProfile: *profile})
		if *ami == "" && args[0] == "exit-create" {
			fail("--ami required")
		}
		if args[0] != "exit-create" {
			p, err = nibaws.New(api, nibaws.Config{Env: *env, AMI: "x", SubnetID: n.SubnetID, SecurityGrp: n.ExitSG})
		}
		check(err)
		switch args[0] {
		case "exit-create":
			ci, err := boot.ExitCloudInit(boot.ExitParams{Slot: *slot, PrivateKey: *priv, HubPublic: *hubPub, HubEndpoint: *hubIP, LeaseMinutes: *lease})
			check(err)
			e, err := p.CreateExit(ctx, provider.ExitSpec{Env: *env, Slot: *slot, Token: fmt.Sprintf("%s/dev/%d/%d", *env, *slot, time.Now().Unix()), CloudInit: ci, Size: "t3.nano"})
			check(err)
			fmt.Println(e.ID)
		case "exit-list":
			es, err := p.ListExits(ctx, *env)
			check(err)
			for _, e := range es {
				fmt.Println(e.ID, e.Slot, e.State, e.PublicIP)
			}
		case "exit-delete":
			check(p.DeleteExit(ctx, *id))
			fmt.Println("deleted", *id)
		default:
			fail("unknown dev command %q", args[0])
		}
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

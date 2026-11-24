// Command niblr is the operator/admin CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	nibaws "niblr/internal/provider/aws"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: niblr init [--env NAME] [--region R] [--az AZ]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "dev":
		dev(os.Args[2:])
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		env := fs.String("env", "default", "environment name")
		region := fs.String("region", "us-west-1", "AWS region")
		az := fs.String("az", "", "availability zone (default: <region>a)")
		fs.Parse(os.Args[2:])
		if *az == "" {
			*az = *region + "a"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(*region))
		if err != nil {
			fail("Could not load AWS credentials: %v", err)
		}
		n, err := nibaws.EnsureNetwork(ctx, ec2.NewFromConfig(cfg), *env, *az)
		if err != nil {
			fail("Network setup failed: %v", err)
		}
		fmt.Printf("NiBLR network ready in %s (env %q)\n  vpc %s\n  subnet %s\n  igw %s\n  exit-sg %s\n  hub-sg %s\n",
			*region, *env, n.VPCID, n.SubnetID, n.IGWID, n.ExitSG, n.HubSG)
	default:
		fail("unknown command %q", os.Args[1])
	}
}

func fail(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }

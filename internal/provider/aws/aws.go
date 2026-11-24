// Package aws implements provider.Provider on EC2.
//
// Safety model (the account is SHARED with things that are not NiBLR's):
//   - every instance NiBLR creates is tagged niblr-managed=true + niblr-env + niblr-slot AT CREATE TIME
//   - listing only ever returns instances with niblr-managed=true AND this provider's env
//   - DeleteExit re-reads the instance's tags and refuses unless they match, even though IAM also
//     restricts terminate to niblr-managed instances (defense in depth: a bug here must not be able to
//     delete someone else's instance)
package aws

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	"niblr/internal/provider"
)

// TagManaged marks everything NiBLR created. It is the trust boundary used by IAM and by this package.
const TagManaged = "niblr-managed"

// API is the subset of the EC2 client we use. *ec2.Client satisfies it; tests use a stub.
type API interface {
	RunInstances(ctx context.Context, in *ec2.RunInstancesInput, opts ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	TerminateInstances(ctx context.Context, in *ec2.TerminateInstancesInput, opts ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
}

// Config is everything an AWS provider needs, all created beforehand by `niblr init`.
type Config struct {
	Env         string // this provider instance only ever sees/touches this environment
	AMI         string
	SubnetID    string
	SecurityGrp string
	DefaultSize string // instance type when ExitSpec.Size is empty
}

type Provider struct {
	api API
	cfg Config
}

func New(api API, cfg Config) (*Provider, error) {
	switch {
	case cfg.Env == "":
		return nil, errors.New("aws: Env is required")
	case cfg.AMI == "", cfg.SubnetID == "", cfg.SecurityGrp == "":
		return nil, errors.New("aws: AMI, SubnetID and SecurityGrp are required (run `niblr init`)")
	}
	if cfg.DefaultSize == "" {
		cfg.DefaultSize = "t3.nano"
	}
	return &Provider{api: api, cfg: cfg}, nil
}

func (p *Provider) Name() string { return "aws" }

// clientToken makes RunInstances idempotent. EC2 tokens are max 64 ASCII chars, so hash ours.
func clientToken(token string) string {
	if token == "" {
		return ""
	}
	h := sha256.Sum256([]byte(token))
	return "niblr-" + hex.EncodeToString(h[:])[:32]
}

func tags(env string, slot int) []types.Tag {
	return []types.Tag{
		{Key: strp(TagManaged), Value: strp("true")},
		{Key: strp(provider.TagEnv), Value: strp(env)},
		{Key: strp(provider.TagSlot), Value: strp(strconv.Itoa(slot))},
		{Key: strp(provider.TagRole), Value: strp("exit")},
		{Key: strp("Name"), Value: strp(fmt.Sprintf("niblr-%s-exit%d", env, slot))},
	}
}

func (p *Provider) CreateExit(ctx context.Context, s provider.ExitSpec) (provider.Exit, error) {
	if s.Env != p.cfg.Env {
		return provider.Exit{}, fmt.Errorf("aws: refusing to create in env %q (provider is bound to %q)", s.Env, p.cfg.Env)
	}
	if s.Slot < 1 {
		return provider.Exit{}, fmt.Errorf("aws: invalid slot %d", s.Slot)
	}
	size := s.Size
	if size == "" {
		size = p.cfg.DefaultSize
	}
	t := tags(s.Env, s.Slot)
	in := &ec2.RunInstancesInput{
		ImageId:      &p.cfg.AMI,
		InstanceType: types.InstanceType(size),
		MinCount:     int32p(1),
		MaxCount:     int32p(1),
		ClientToken:  strp(clientToken(s.Token)),
		UserData:     strp(base64.StdEncoding.EncodeToString([]byte(s.CloudInit))),
		// Subnet and SG live inside the interface spec because we ask for a public IP.
		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{{
			DeviceIndex:              int32p(0),
			AssociatePublicIpAddress: boolp(true),
			SubnetId:                 &p.cfg.SubnetID,
			Groups:                   []string{p.cfg.SecurityGrp},
			DeleteOnTermination:      boolp(true),
		}},
		MetadataOptions: &types.InstanceMetadataOptionsRequest{HttpTokens: types.HttpTokensStateRequired},
		// Tag at create time, atomically. Tagging afterwards would leave a crash window with an orphan.
		TagSpecifications: []types.TagSpecification{
			{ResourceType: types.ResourceTypeInstance, Tags: t},
			{ResourceType: types.ResourceTypeVolume, Tags: t},
			{ResourceType: types.ResourceTypeNetworkInterface, Tags: t},
		},
		InstanceInitiatedShutdownBehavior: types.ShutdownBehaviorTerminate, // lease: a shutdown inside the box frees it
	}
	out, err := p.api.RunInstances(ctx, in)
	if err != nil {
		return provider.Exit{}, fmt.Errorf("aws: run instance for slot %d: %w", s.Slot, err)
	}
	if len(out.Instances) != 1 {
		return provider.Exit{}, fmt.Errorf("aws: expected 1 instance, got %d", len(out.Instances))
	}
	return toExit(out.Instances[0]), nil
}

// ListExits returns NiBLR-managed instances of this env only, including pending ones (so strays are seen).
func (p *Provider) ListExits(ctx context.Context, env string) ([]provider.Exit, error) {
	if env != p.cfg.Env {
		return nil, fmt.Errorf("aws: provider is bound to env %q, not %q", p.cfg.Env, env)
	}
	in := &ec2.DescribeInstancesInput{Filters: []types.Filter{
		{Name: strp("tag:" + TagManaged), Values: []string{"true"}},
		{Name: strp("tag:" + provider.TagEnv), Values: []string{env}},
		{Name: strp("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped", "shutting-down"}},
	}}
	var out []provider.Exit
	for {
		page, err := p.api.DescribeInstances(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("aws: describe instances: %w", err)
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				out = append(out, toExit(i))
			}
		}
		if page.NextToken == nil {
			return out, nil
		}
		in.NextToken = page.NextToken
	}
}

// DeleteExit terminates one instance, but only after confirming it is ours and in this env.
func (p *Provider) DeleteExit(ctx context.Context, id string) error {
	d, err := p.api.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "InvalidInstanceID.NotFound" {
			return nil // already gone is success
		}
		return fmt.Errorf("aws: describe %s before delete: %w", id, err)
	}
	var inst *types.Instance
	for _, r := range d.Reservations {
		for i := range r.Instances {
			if r.Instances[i].InstanceId != nil && *r.Instances[i].InstanceId == id {
				inst = &r.Instances[i]
			}
		}
	}
	if inst == nil {
		return nil
	}
	tg := tagMap(inst.Tags)
	if tg[TagManaged] != "true" || tg[provider.TagEnv] != p.cfg.Env {
		return fmt.Errorf("aws: refusing to delete %s: not a NiBLR instance of env %q (tags: managed=%q env=%q)",
			id, p.cfg.Env, tg[TagManaged], tg[provider.TagEnv])
	}
	if inst.State != nil && (inst.State.Name == types.InstanceStateNameTerminated || inst.State.Name == types.InstanceStateNameShuttingDown) {
		return nil
	}
	if _, err := p.api.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: []string{id}}); err != nil {
		return fmt.Errorf("aws: terminate %s: %w", id, err)
	}
	return nil
}

func toExit(i types.Instance) provider.Exit {
	tg := tagMap(i.Tags)
	slot, err := strconv.Atoi(tg[provider.TagSlot])
	if err != nil {
		slot = 0 // unparseable slot => stray; the reconciler deletes slot<1
	}
	e := provider.Exit{Env: tg[provider.TagEnv], Slot: slot, State: mapState(i.State)}
	if i.InstanceId != nil {
		e.ID = *i.InstanceId
	}
	if i.PublicIpAddress != nil {
		e.PublicIP = *i.PublicIpAddress
	}
	if i.LaunchTime != nil {
		e.Created = *i.LaunchTime
	} else {
		e.Created = time.Time{}
	}
	return e
}

func mapState(s *types.InstanceState) provider.State {
	if s == nil {
		return provider.StateGone
	}
	switch s.Name {
	case types.InstanceStateNamePending:
		return provider.StatePending
	case types.InstanceStateNameRunning:
		return provider.StateRunning
	case types.InstanceStateNameStopping, types.InstanceStateNameStopped:
		return provider.StateBroken // still billed (disk), never a working exit: delete and replace
	default: // shutting-down, terminated
		return provider.StateGone
	}
}

func tagMap(ts []types.Tag) map[string]string {
	m := map[string]string{}
	for _, t := range ts {
		if t.Key != nil && t.Value != nil {
			m[*t.Key] = *t.Value
		}
	}
	return m
}

func strp(s string) *string { return &s }
func int32p(i int32) *int32 { return &i }
func boolp(b bool) *bool    { return &b }

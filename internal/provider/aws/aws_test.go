package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	"niblr/internal/provider"
)

type stub struct {
	run        []*ec2.RunInstancesInput
	describe   []*ec2.DescribeInstancesInput
	terminated []string
	pages      []*ec2.DescribeInstancesOutput // returned in order for describe calls
	byID       map[string]types.Instance
	runErr     error
}

func (s *stub) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	s.run = append(s.run, in)
	if s.runErr != nil {
		return nil, s.runErr
	}
	return &ec2.RunInstancesOutput{Instances: []types.Instance{{InstanceId: strp("i-new"), State: &types.InstanceState{Name: types.InstanceStateNamePending}, Tags: tags("eng1", 2)}}}, nil
}

func (s *stub) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	s.describe = append(s.describe, in)
	if len(in.InstanceIds) == 1 {
		i, ok := s.byID[in.InstanceIds[0]]
		if !ok {
			return nil, &smithy.GenericAPIError{Code: "InvalidInstanceID.NotFound", Message: "nope"}
		}
		return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{i}}}}, nil
	}
	p := s.pages[0]
	s.pages = s.pages[1:]
	return p, nil
}

func (s *stub) TerminateInstances(_ context.Context, in *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	s.terminated = append(s.terminated, in.InstanceIds...)
	return &ec2.TerminateInstancesOutput{}, nil
}

func newProv(t *testing.T, s *stub) *Provider {
	t.Helper()
	p, err := New(s, Config{Env: "eng1", AMI: "ami-1", SubnetID: "subnet-1", SecurityGrp: "sg-1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func inst(id string, state types.InstanceStateName, tg ...string) types.Instance {
	var ts []types.Tag
	for i := 0; i+1 < len(tg); i += 2 {
		ts = append(ts, types.Tag{Key: strp(tg[i]), Value: strp(tg[i+1])})
	}
	return types.Instance{InstanceId: strp(id), State: &types.InstanceState{Name: state}, Tags: ts}
}

func TestCreateTagsAtCreateTimeAndHardensInstance(t *testing.T) {
	s := &stub{}
	p := newProv(t, s)
	_, err := p.CreateExit(context.Background(), provider.ExitSpec{Env: "eng1", Slot: 2, Token: "eng1/r1/2", CloudInit: "#cloud-config\nhello"})
	if err != nil {
		t.Fatal(err)
	}
	in := s.run[0]
	// Tags on instance, volume AND network interface, applied in the same call.
	kinds := map[types.ResourceType]bool{}
	for _, ts := range in.TagSpecifications {
		kinds[ts.ResourceType] = true
		m := tagMap(ts.Tags)
		if m[TagManaged] != "true" || m[provider.TagEnv] != "eng1" || m[provider.TagSlot] != "2" {
			t.Fatalf("incomplete tags on %s: %v", ts.ResourceType, m)
		}
	}
	for _, k := range []types.ResourceType{types.ResourceTypeInstance, types.ResourceTypeVolume, types.ResourceTypeNetworkInterface} {
		if !kinds[k] {
			t.Fatalf("no tags at create time for %s (orphan window)", k)
		}
	}
	if in.MetadataOptions == nil || in.MetadataOptions.HttpTokens != types.HttpTokensStateRequired {
		t.Fatal("IMDSv2 must be required")
	}
	if in.InstanceInitiatedShutdownBehavior != types.ShutdownBehaviorTerminate {
		t.Fatal("shutdown must terminate (lease)")
	}
	ni := in.NetworkInterfaces[0]
	if *ni.SubnetId != "subnet-1" || ni.Groups[0] != "sg-1" || !*ni.AssociatePublicIpAddress {
		t.Fatalf("must launch into the NiBLR subnet/SG with a public IP: %+v", ni)
	}
	raw, _ := base64.StdEncoding.DecodeString(*in.UserData)
	if !strings.Contains(string(raw), "hello") {
		t.Fatal("user data not passed through base64-encoded")
	}
}

func TestClientTokenIsDeterministicAndShortEnough(t *testing.T) {
	a, b := clientToken("eng1/r1/2"), clientToken("eng1/r1/2")
	if a != b || a == clientToken("eng1/r1/3") {
		t.Fatal("token must be stable per input and differ per slot")
	}
	if len(clientToken(strings.Repeat("x", 500))) > 64 {
		t.Fatal("EC2 client tokens are limited to 64 chars")
	}
	if clientToken("") != "" {
		t.Fatal("empty token must stay empty")
	}
}

func TestCreateRefusesForeignEnvAndBadSlot(t *testing.T) {
	s := &stub{}
	p := newProv(t, s)
	if _, err := p.CreateExit(context.Background(), provider.ExitSpec{Env: "other", Slot: 1}); err == nil {
		t.Fatal("must not create in a different env")
	}
	if _, err := p.CreateExit(context.Background(), provider.ExitSpec{Env: "eng1", Slot: 0}); err == nil {
		t.Fatal("slot 0 is invalid")
	}
	if len(s.run) != 0 {
		t.Fatal("no API call should have been made")
	}
}

func TestListFiltersToManagedAndEnvAndPaginates(t *testing.T) {
	s := &stub{pages: []*ec2.DescribeInstancesOutput{
		{Reservations: []types.Reservation{{Instances: []types.Instance{inst("i-1", types.InstanceStateNameRunning, provider.TagSlot, "1", provider.TagEnv, "eng1", TagManaged, "true")}}}, NextToken: strp("t2")},
		{Reservations: []types.Reservation{{Instances: []types.Instance{inst("i-2", types.InstanceStateNameStopped, provider.TagSlot, "2", provider.TagEnv, "eng1", TagManaged, "true"), inst("i-3", types.InstanceStateNamePending, provider.TagSlot, "bad", provider.TagEnv, "eng1", TagManaged, "true")}}}},
	}}
	got, err := newProv(t, s).ListExits(context.Background(), "eng1")
	if err != nil || len(got) != 3 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	f := map[string][]string{}
	for _, fl := range s.describe[0].Filters {
		f[*fl.Name] = fl.Values
	}
	if f["tag:"+TagManaged][0] != "true" || f["tag:"+provider.TagEnv][0] != "eng1" {
		t.Fatalf("must filter to managed + env on the server side: %v", f)
	}
	if got[1].State != provider.StateBroken {
		t.Fatalf("stopped must be broken (billed, to be deleted), got %v", got[1].State)
	}
	if got[2].Slot != 0 {
		t.Fatalf("unparseable slot must become 0 (stray), got %d", got[2].Slot)
	}
	if s.describe[1].NextToken == nil || *s.describe[1].NextToken != "t2" {
		t.Fatal("pagination token not passed on")
	}
}

func TestListRefusesWrongEnv(t *testing.T) {
	if _, err := newProv(t, &stub{}).ListExits(context.Background(), "other"); err == nil {
		t.Fatal("provider is bound to one env")
	}
}

func TestDeleteRefusesInstancesThatAreNotOurs(t *testing.T) {
	// The shared-account safety test: other teams' instances must never be terminated.
	s := &stub{byID: map[string]types.Instance{
		"i-theirs":  inst("i-theirs", types.InstanceStateNameRunning),                                              // no tags at all
		"i-spoof":   inst("i-spoof", types.InstanceStateNameRunning, TagManaged, "true", provider.TagEnv, "other"), // ours, but other env
		"i-half":    inst("i-half", types.InstanceStateNameRunning, provider.TagEnv, "eng1"),                       // env tag but not managed
		"i-managed": inst("i-managed", types.InstanceStateNameRunning, TagManaged, "true", provider.TagEnv, "eng1"),
		"i-dying":   inst("i-dying", types.InstanceStateNameShuttingDown, TagManaged, "true", provider.TagEnv, "eng1"),
	}}
	p := newProv(t, s)
	for _, id := range []string{"i-theirs", "i-spoof", "i-half"} {
		if err := p.DeleteExit(context.Background(), id); err == nil {
			t.Fatalf("%s: must refuse", id)
		}
	}
	if len(s.terminated) != 0 {
		t.Fatalf("terminated something that is not ours: %v", s.terminated)
	}
	if err := p.DeleteExit(context.Background(), "i-managed"); err != nil || len(s.terminated) != 1 || s.terminated[0] != "i-managed" {
		t.Fatalf("should terminate our own: err=%v terminated=%v", err, s.terminated)
	}
	if err := p.DeleteExit(context.Background(), "i-dying"); err != nil || len(s.terminated) != 1 {
		t.Fatalf("already shutting down is success without another call: err=%v %v", err, s.terminated)
	}
}

func TestDeleteOfMissingInstanceIsSuccess(t *testing.T) {
	s := &stub{byID: map[string]types.Instance{}}
	if err := newProv(t, s).DeleteExit(context.Background(), "i-gone"); err != nil || len(s.terminated) != 0 {
		t.Fatalf("err=%v terminated=%v", err, s.terminated)
	}
}

func TestCreateErrorIsWrappedNotSwallowed(t *testing.T) {
	boom := errors.New("VcpuLimitExceeded")
	_, err := newProv(t, &stub{runErr: boom}).CreateExit(context.Background(), provider.ExitSpec{Env: "eng1", Slot: 1})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the cause to be preserved, got %v", err)
	}
	_ = time.Now
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(&stub{}, Config{Env: "e"}); err == nil {
		t.Fatal("must require AMI/subnet/SG")
	}
	if _, err := New(&stub{}, Config{AMI: "a", SubnetID: "s", SecurityGrp: "g"}); err == nil {
		t.Fatal("must require Env")
	}
}

func TestMetaRoundTripsThroughTags(t *testing.T) {
	ts := tags("eng1", 4, map[string]string{"pub": "KEY="})
	e := toExit(types.Instance{Tags: ts, State: &types.InstanceState{Name: types.InstanceStateNameRunning}})
	if e.Slot != 4 || e.Meta["pub"] != "KEY=" {
		t.Fatalf("meta lost: %+v", e)
	}
}

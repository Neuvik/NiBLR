package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// netStub is a minimal stateful EC2: it honours the tag filters EnsureNetwork relies on.
type netStub struct {
	vpcs, igws, subnets, sgs int
	routes                   int
	otherVpc                 bool // an untagged, foreign VPC that must be ignored
	vpcTagged                bool
}

func (s *netStub) DescribeVpcs(_ context.Context, in *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if s.vpcs == 0 {
		return &ec2.DescribeVpcsOutput{}, nil // foreign VPC is untagged → filtered out server side
	}
	return &ec2.DescribeVpcsOutput{Vpcs: []types.Vpc{{VpcId: strp("vpc-1")}}}, nil
}
func (s *netStub) CreateVpc(_ context.Context, in *ec2.CreateVpcInput, _ ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
	s.vpcs++
	s.vpcTagged = len(in.TagSpecifications) == 1
	return &ec2.CreateVpcOutput{Vpc: &types.Vpc{VpcId: strp("vpc-1")}}, nil
}
func (s *netStub) DescribeInternetGateways(context.Context, *ec2.DescribeInternetGatewaysInput, ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
	if s.igws == 0 {
		return &ec2.DescribeInternetGatewaysOutput{}, nil
	}
	return &ec2.DescribeInternetGatewaysOutput{InternetGateways: []types.InternetGateway{{InternetGatewayId: strp("igw-1"), Attachments: []types.InternetGatewayAttachment{{VpcId: strp("vpc-1")}}}}}, nil
}
func (s *netStub) CreateInternetGateway(context.Context, *ec2.CreateInternetGatewayInput, ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error) {
	s.igws++
	return &ec2.CreateInternetGatewayOutput{InternetGateway: &types.InternetGateway{InternetGatewayId: strp("igw-1")}}, nil
}
func (s *netStub) AttachInternetGateway(context.Context, *ec2.AttachInternetGatewayInput, ...func(*ec2.Options)) (*ec2.AttachInternetGatewayOutput, error) {
	return &ec2.AttachInternetGatewayOutput{}, nil
}
func (s *netStub) DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if s.subnets == 0 {
		return &ec2.DescribeSubnetsOutput{}, nil
	}
	return &ec2.DescribeSubnetsOutput{Subnets: []types.Subnet{{SubnetId: strp("subnet-1")}}}, nil
}
func (s *netStub) CreateSubnet(context.Context, *ec2.CreateSubnetInput, ...func(*ec2.Options)) (*ec2.CreateSubnetOutput, error) {
	s.subnets++
	return &ec2.CreateSubnetOutput{Subnet: &types.Subnet{SubnetId: strp("subnet-1")}}, nil
}
func (s *netStub) DescribeRouteTables(context.Context, *ec2.DescribeRouteTablesInput, ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	rt := types.RouteTable{RouteTableId: strp("rtb-1")}
	if s.routes > 0 {
		rt.Routes = []types.Route{{DestinationCidrBlock: strp("0.0.0.0/0"), GatewayId: strp("igw-1")}}
	}
	return &ec2.DescribeRouteTablesOutput{RouteTables: []types.RouteTable{rt}}, nil
}
func (s *netStub) CreateRoute(context.Context, *ec2.CreateRouteInput, ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error) {
	s.routes++
	return &ec2.CreateRouteOutput{}, nil
}
func (s *netStub) DescribeSecurityGroups(_ context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if s.sgs < 2 {
		return &ec2.DescribeSecurityGroupsOutput{}, nil
	}
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []types.SecurityGroup{{GroupId: strp("sg-x")}}}, nil
}
func (s *netStub) CreateSecurityGroup(context.Context, *ec2.CreateSecurityGroupInput, ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
	s.sgs++
	return &ec2.CreateSecurityGroupOutput{GroupId: strp("sg-new")}, nil
}
func (s *netStub) AuthorizeSecurityGroupIngress(context.Context, *ec2.AuthorizeSecurityGroupIngressInput, ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error) {
	return &ec2.AuthorizeSecurityGroupIngressOutput{}, nil
}

func TestEnsureNetworkCreatesTaggedThenIsIdempotent(t *testing.T) {
	s := &netStub{}
	n, err := EnsureNetwork(context.Background(), s, "demo", "us-west-1a")
	if err != nil {
		t.Fatal(err)
	}
	if n.VPCID == "" || n.SubnetID == "" || n.IGWID == "" || n.ExitSG == "" || n.HubSG == "" {
		t.Fatalf("incomplete network: %+v", n)
	}
	if !s.vpcTagged {
		t.Fatal("VPC must be tagged at create time")
	}
	before := *s
	if _, err := EnsureNetwork(context.Background(), s, "demo", "us-west-1a"); err != nil {
		t.Fatal(err)
	}
	if s.vpcs != before.vpcs || s.igws != before.igws || s.subnets != before.subnets || s.routes != before.routes || s.sgs != before.sgs {
		t.Fatalf("second run created things: before=%+v after=%+v", before, *s)
	}
}

func (s *netStub) RevokeSecurityGroupIngress(context.Context, *ec2.RevokeSecurityGroupIngressInput, ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error) {
	return &ec2.RevokeSecurityGroupIngressOutput{}, nil
}

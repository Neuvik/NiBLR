package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	"niblr/internal/provider"
)

// NetAPI is the subset of EC2 used to bootstrap NiBLR's own network.
type NetAPI interface {
	DescribeVpcs(ctx context.Context, in *ec2.DescribeVpcsInput, opts ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	CreateVpc(ctx context.Context, in *ec2.CreateVpcInput, opts ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error)
	DescribeInternetGateways(ctx context.Context, in *ec2.DescribeInternetGatewaysInput, opts ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error)
	CreateInternetGateway(ctx context.Context, in *ec2.CreateInternetGatewayInput, opts ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error)
	AttachInternetGateway(ctx context.Context, in *ec2.AttachInternetGatewayInput, opts ...func(*ec2.Options)) (*ec2.AttachInternetGatewayOutput, error)
	DescribeSubnets(ctx context.Context, in *ec2.DescribeSubnetsInput, opts ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	CreateSubnet(ctx context.Context, in *ec2.CreateSubnetInput, opts ...func(*ec2.Options)) (*ec2.CreateSubnetOutput, error)
	DescribeRouteTables(ctx context.Context, in *ec2.DescribeRouteTablesInput, opts ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	CreateRoute(ctx context.Context, in *ec2.CreateRouteInput, opts ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error)
	DescribeSecurityGroups(ctx context.Context, in *ec2.DescribeSecurityGroupsInput, opts ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	CreateSecurityGroup(ctx context.Context, in *ec2.CreateSecurityGroupInput, opts ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	AuthorizeSecurityGroupIngress(ctx context.Context, in *ec2.AuthorizeSecurityGroupIngressInput, opts ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	RevokeSecurityGroupIngress(ctx context.Context, in *ec2.RevokeSecurityGroupIngressInput, opts ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error)
}

// Network is what `niblr init` produces and the provider needs.
type Network struct {
	VPCID, SubnetID, IGWID string
	ExitSG, HubSG          string
}

const (
	vpcCIDR    = "10.77.0.0/16"
	subnetCIDR = "10.77.0.0/24"
	// The hub listens on one WireGuard port per interface: operators on WGPortFrom, exit slot N on
	// ExitPortBase+N. All inside the range the hub security group opens.
	WGPortFrom   = 51000
	WGPortTo     = 51999
	ExitPortBase = 51100
)

func tagSpec(rt types.ResourceType, env, name string) []types.TagSpecification {
	return []types.TagSpecification{{ResourceType: rt, Tags: []types.Tag{
		{Key: strp(TagManaged), Value: strp("true")},
		{Key: strp(provider.TagEnv), Value: strp(env)},
		{Key: strp("Name"), Value: strp(name)},
	}}}
}

func envFilters(env string, extra ...types.Filter) []types.Filter {
	return append([]types.Filter{
		{Name: strp("tag:" + TagManaged), Values: []string{"true"}},
		{Name: strp("tag:" + provider.TagEnv), Values: []string{env}},
	}, extra...)
}

// EnsureNetwork creates (or finds) NiBLR's dedicated VPC, internet gateway, subnet, route and security
// groups. It is idempotent and only ever looks at resources tagged niblr-managed=true for this env;
// it never touches the account's default VPC or anyone else's resources.
func EnsureNetwork(ctx context.Context, api NetAPI, env, az string) (Network, error) {
	var n Network
	pre := "niblr-" + env

	// VPC
	v, err := api.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{Filters: envFilters(env)})
	if err != nil {
		return n, fmt.Errorf("find vpc: %w", err)
	}
	if len(v.Vpcs) > 0 {
		n.VPCID = *v.Vpcs[0].VpcId
	} else {
		o, err := api.CreateVpc(ctx, &ec2.CreateVpcInput{CidrBlock: strp(vpcCIDR), TagSpecifications: tagSpec(types.ResourceTypeVpc, env, pre)})
		if err != nil {
			return n, fmt.Errorf("create vpc: %w", err)
		}
		n.VPCID = *o.Vpc.VpcId
	}

	// Internet gateway
	g, err := api.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{Filters: envFilters(env)})
	if err != nil {
		return n, fmt.Errorf("find igw: %w", err)
	}
	attached := false
	if len(g.InternetGateways) > 0 {
		ig := g.InternetGateways[0]
		n.IGWID = *ig.InternetGatewayId
		for _, a := range ig.Attachments {
			if a.VpcId != nil && *a.VpcId == n.VPCID {
				attached = true
			}
		}
	} else {
		o, err := api.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{TagSpecifications: tagSpec(types.ResourceTypeInternetGateway, env, pre)})
		if err != nil {
			return n, fmt.Errorf("create igw: %w", err)
		}
		n.IGWID = *o.InternetGateway.InternetGatewayId
	}
	if !attached {
		if _, err := api.AttachInternetGateway(ctx, &ec2.AttachInternetGatewayInput{InternetGatewayId: &n.IGWID, VpcId: &n.VPCID}); err != nil && !isCode(err, "Resource.AlreadyAssociated") {
			return n, fmt.Errorf("attach igw: %w", err)
		}
	}

	// Subnet
	s, err := api.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{Filters: envFilters(env, types.Filter{Name: strp("vpc-id"), Values: []string{n.VPCID}})})
	if err != nil {
		return n, fmt.Errorf("find subnet: %w", err)
	}
	if len(s.Subnets) > 0 {
		n.SubnetID = *s.Subnets[0].SubnetId
	} else {
		o, err := api.CreateSubnet(ctx, &ec2.CreateSubnetInput{VpcId: &n.VPCID, CidrBlock: strp(subnetCIDR), AvailabilityZone: strp(az), TagSpecifications: tagSpec(types.ResourceTypeSubnet, env, pre)})
		if err != nil {
			return n, fmt.Errorf("create subnet: %w", err)
		}
		n.SubnetID = *o.Subnet.SubnetId
	}

	// Default route via the IGW, in the VPC's main route table (ours alone, so no one else is affected).
	rt, err := api.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{Filters: []types.Filter{
		{Name: strp("vpc-id"), Values: []string{n.VPCID}},
		{Name: strp("association.main"), Values: []string{"true"}},
	}})
	if err != nil || len(rt.RouteTables) == 0 {
		return n, fmt.Errorf("find route table: %v", err)
	}
	hasDefault := false
	for _, r := range rt.RouteTables[0].Routes {
		if r.DestinationCidrBlock != nil && *r.DestinationCidrBlock == "0.0.0.0/0" && r.GatewayId != nil && *r.GatewayId == n.IGWID {
			hasDefault = true
		}
	}
	if !hasDefault {
		if _, err := api.CreateRoute(ctx, &ec2.CreateRouteInput{RouteTableId: rt.RouteTables[0].RouteTableId, DestinationCidrBlock: strp("0.0.0.0/0"), GatewayId: &n.IGWID}); err != nil && !isCode(err, "RouteAlreadyExists") {
			return n, fmt.Errorf("create route: %w", err)
		}
	}

	// Security groups: exits accept nothing inbound (they dial the hub); the hub accepts only WireGuard.
	if n.ExitSG, err = ensureSG(ctx, api, env, n.VPCID, pre+"-exit", "NiBLR exits: no inbound", false); err != nil {
		return n, err
	}
	if n.HubSG, err = ensureSG(ctx, api, env, n.VPCID, pre+"-hub", "NiBLR hub: WireGuard only", true); err != nil {
		return n, err
	}
	return n, nil
}

func ensureSG(ctx context.Context, api NetAPI, env, vpc, name, desc string, wireguard bool) (string, error) {
	d, err := api.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{Filters: envFilters(env,
		types.Filter{Name: strp("vpc-id"), Values: []string{vpc}},
		types.Filter{Name: strp("group-name"), Values: []string{name}})})
	if err != nil {
		return "", fmt.Errorf("find sg %s: %w", name, err)
	}
	var id string
	if len(d.SecurityGroups) > 0 {
		id = *d.SecurityGroups[0].GroupId
	} else {
		o, err := api.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{GroupName: &name, Description: &desc, VpcId: &vpc, TagSpecifications: tagSpec(types.ResourceTypeSecurityGroup, env, name)})
		if err != nil {
			return "", fmt.Errorf("create sg %s: %w", name, err)
		}
		id = *o.GroupId
	}
	if wireguard {
		// Always (re)assert the rule: adding is idempotent, and this repairs groups made by older versions.
		_, err = api.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{GroupId: &id, IpPermissions: []types.IpPermission{{
			IpProtocol: strp("udp"), FromPort: int32p(WGPortFrom), ToPort: int32p(WGPortTo),
			IpRanges: []types.IpRange{{CidrIp: strp("0.0.0.0/0"), Description: strp("WireGuard")}},
		}}})
		if err != nil && !isCode(err, "InvalidPermission.Duplicate") {
			return "", fmt.Errorf("open wireguard ports on %s: %w", name, err)
		}
		// Close the single-port rule an earlier version created.
		_, err = api.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{GroupId: &id, IpPermissions: []types.IpPermission{{
			IpProtocol: strp("udp"), FromPort: int32p(51820), ToPort: int32p(51820),
			IpRanges: []types.IpRange{{CidrIp: strp("0.0.0.0/0")}},
		}}})
		if err != nil && !isCode(err, "InvalidPermission.NotFound") {
			return "", fmt.Errorf("close old port on %s: %w", name, err)
		}
	}
	return id, nil
}

func isCode(err error, code string) bool {
	var ae smithy.APIError
	return errors.As(err, &ae) && ae.ErrorCode() == code
}

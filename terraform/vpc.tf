# The main VPC for all nodes and systems in a single Availability Zone
resource "aws_vpc" "vpc" {
  cidr_block           = "10.1.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name       = "RedTeam-VPC"
    Assessment = "RedTeam Overhead"
  }
}

# This is the main internet gateway although each system should have its own IP
resource "aws_internet_gateway" "gateway" {
  vpc_id = aws_vpc.vpc.id

  tags = {
    Name       = "RedTeam-IGW"
    Assessment = "RedTeam Overhead"
  }
}

# Routing table for VPC
resource "aws_route_table" "public_route_table" {
  vpc_id = aws_vpc.vpc.id
  tags = {
    Name = "public-route-table-a"
  }
}

# Default Gateway
resource "aws_route" "public_subnet" {
  route_table_id         = aws_route_table.public_route_table.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.gateway.id
}

# Public Subnet Association
resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public_subnet.id
  route_table_id = aws_route_table.public_route_table.id
}

# Main subnet
resource "aws_subnet" "public_subnet" {
  vpc_id                  = aws_vpc.vpc.id
  cidr_block              = "10.1.0.0/24"
  map_public_ip_on_launch = true
  availability_zone       = "${var.aws_region}a"

  tags = {
    Name       = "RedTeam Public Subnet A"
    Assessment = "RedTeam Overhead"
  }
}
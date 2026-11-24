# Throwaway test rig for the NiBLR data-plane proof: 1 hub, 2 exits, 1 client. All t3.nano.
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}
variable "region" {
  default = "us-east-2"
}
variable "my_cidr" {
  description = "Your public IP /32, allowed to SSH"
}
variable "pubkey" {
  default = "~/.ssh/id_ed25519.pub"
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "niblr-proto" }
  }
}

data "aws_vpc" "default" {
  default = true
}
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"]
  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }
}

resource "aws_key_pair" "k" {
  key_name   = "niblr-proto"
  public_key = file(pathexpand(var.pubkey))
}

resource "aws_security_group" "rig" {
  name        = "niblr-proto"
  description = "NiBLR proto: ssh from operator IP, wireguard udp, all intra-rig"
  vpc_id      = data.aws_vpc.default.id
  ingress {
    description = "ssh from operator"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.my_cidr]
  }
  ingress {
    description = "wireguard (peers are key-authenticated)"
    from_port   = 51000
    to_port     = 51010
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    description = "intra-rig"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    self        = true
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# Second way in (independent of SSH and the IP allowlist): AWS Systems Manager.
resource "aws_iam_role" "ssm" {
  name = "niblr-proto-ssm"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.ssm.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ssm" {
  name = "niblr-proto-ssm"
  role = aws_iam_role.ssm.name
}

locals {
  nodes = toset(["hub", "exit1", "exit2", "client", "target"])
}

resource "aws_instance" "n" {
  for_each                    = local.nodes
  ami                         = data.aws_ami.ubuntu.id
  instance_type               = "t3.nano"
  key_name                    = aws_key_pair.k.key_name
  iam_instance_profile        = aws_iam_instance_profile.ssm.name
  vpc_security_group_ids      = [aws_security_group.rig.id]
  associate_public_ip_address = true
  root_block_device {
    volume_size = 8
  }
  user_data = <<-EOT
    #!/bin/bash
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -q && apt-get install -y -q wireguard nftables conntrack python3 tcpdump
    touch /var/lib/niblr-ready
  EOT
  tags = { Name = "niblr-proto-${each.key}" }
}

output "ips" {
  value = { for k, v in aws_instance.n : k => { public = v.public_ip, private = v.private_ip } }
}

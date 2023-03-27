resource "aws_security_group" "default" {
  name        = "main-allow-defaults"
  description = "Allow SSH inbound traffic, from servers within the environment."
  vpc_id      = aws_vpc.vpc.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.operators_ips
  }

  ingress {
    from_port = 22
    to_port   = 22
    protocol  = "tcp"
    cidr_blocks = [
      aws_vpc.vpc.cidr_block
    ]
  }

  ingress {
    from_port   = 50000
    to_port     = 50000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 50001
    to_port     = 50001
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Assessment = "RedTeam Overhead"
  }
}
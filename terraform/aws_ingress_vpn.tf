resource "aws_instance" "aws_ingress_vpn" {
  ami           = data.aws_ami.ubuntu.id # This is a standard Ubuntu AMI
  instance_type = "t2.micro"             # Micro server for Ingress VPN

  subnet_id = aws_subnet.public_subnet.id # This is the main availabilty zone subnet group

  vpc_security_group_ids = [
    aws_security_group.default.id # This is a standard security group
  ]

  root_block_device {
    volume_size = 50 # This is options, 50GB hard disk may be rather large.
  }

  user_data = data.local_file.cloud_init_ingress_vpn_yaml.content

  tags = {
    Name = "Ingress VPN" # Name
  }

  lifecycle {
    ignore_changes = [
      ami, # Do not remove this, any changes to the Ubuntu AMI will cause this repo to redeploy the machine
      #user_data # Do not remove this, any changes to the User Data will cause this machine to be rebuilt!
    ]
    prevent_destroy = false
  }
}
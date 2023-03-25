#resource "aws_instance" "exit-node" {
#  ami                    = "ami-0f65671a86f061fcd"
#  instance_type          = "t2.micro"
#  vpc_security_group_ids = [aws_security_group.exit-node-sec-group.id]
#  subnet_id              = var.subnet_id
# we need to disable this for internal routing
#  source_dest_check = false
#  count             = var.count


#  tags {
#    Name = "exit-node"
#  }
#}
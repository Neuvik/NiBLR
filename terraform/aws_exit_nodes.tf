resource "local_file" "cloud_init_exit_node_template" {
  content = templatefile("${path.module}/templates/cloud-init-exit-node.tmpl", {
    operators   = var.operators_list,
    node_nums   = var.node_nums
    ansible_key = file("~/.ssh/id_rsa.pub")
  })
  filename = "${path.module}/files/cloud-init-exit-node.yaml"
}

data "local_file" "cloud_init_exit_node_yaml" {
  filename   = local_file.cloud_init_exit_node_template.filename
  depends_on = [local_file.cloud_init_exit_node_template]
}

resource "aws_instance" "exit_node" {
  ami           = data.aws_ami.ubuntu.id # This is a standard Ubuntu AMI
  instance_type = "t2.micro"
  subnet_id     = aws_subnet.public_subnet.id # This is the main availabilty zone subnet group

  vpc_security_group_ids = [
    aws_security_group.default.id # This is a standard security group
  ]

  root_block_device {
    volume_size = 50 # This is options, 50GB hard disk may be rather large.
  }

  user_data = data.local_file.cloud_init_exit_node_yaml.content

  source_dest_check = false
  #count             = 1

  tags = {
    Name = "exit-node"
  }
}
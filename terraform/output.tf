resource "local_file" "aws_ingress_vpn" {
  content = templatefile("${path.module}/templates/inventory.tmpl", {
    cat      = "aws-ingress-vpn",
    ip_addrs = [aws_instance.aws_ingress_vpn.public_ip],
    vars = [
      "ansible_user: ansible",
      "ansible_python_interpreter: /usr/bin/python3",
      "ansible_ssh_private_key_file: ~/.ssh/id_rsa"
    ]
    }
  )
  filename = "${path.module}/../ansible/inventory/aws-ingress-vpn.yml"
}

output "client_conf" {
  value = <<EOF
[Interface]
PrivateKey = ${file("${path.module}/../wireguard_configs/client1.key")}
Address = 10.10.9.2/32
DNS = 8.8.8.8, 8.8.4.4

[Peer]
PublicKey = ${file("${path.module}/../wireguard_configs/wgHub.pub")}
Endpoint = ${aws_instance.aws_ingress_vpn.public_ip}:50000
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 20
EOF
}

output "wgNodeHub_conf" {
  value = <<EOF
[Interface]
Address = 10.10.9.1/24
ListenPort = 50000
PrivateKey = ${file("${path.module}/../wireguard_configs/wgHub.key")}
SaveConfig = true
EOF
}

output "final_text" {
  value = <<EOF
-------------------------------------------------------------------------------
Thank you for using NiBLR, here is the final output for your notes:

Created Ingress Node with IP of: ${aws_instance.aws_ingress_vpn.public_ip}
The IP Addresses of each of the individual ansible nodes will be found in:

The Ansible Inventory List.k
-------------------------------------------------------------------------------
EOF
}
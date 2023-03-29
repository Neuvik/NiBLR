# Configuration for the main VPN server
resource "local_file" "cloud_init_ingress_vpn_template" {
  content = templatefile("${path.module}/templates/cloud-init-ingress-vpn.tmpl", {
    operators          = var.operators_list,
    node_nums          = var.node_nums,
    ansible_key        = file("~/.ssh/id_rsa.pub"),
    wg0_priv_key       = file("${path.module}/../wireguard_configs/wgHub.key"),
    client1_public_key = file("${path.module}/../wireguard_configs/client1.pub"),
    hostname           = var.vpn_hostname
  })
  filename = "${path.module}/files/cloud-init-ingress-vpn.yaml"
}

data "local_file" "cloud_init_ingress_vpn_yaml" {
  filename   = local_file.cloud_init_ingress_vpn_template.filename
  depends_on = [local_file.cloud_init_ingress_vpn_template]
}
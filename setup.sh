#!/bin/bash

set -eu

export TERRAFORM_VERSION="1.4.2"
export OS="$(uname)"
export TERRAFORM_INSTALLED="$(which terraform)"
export EASYRSA_INSTALLED="$(which easyrsa)"
export OPENVPN_INSTALLED="$(which openvpn)"
export CURDIR=$(pwd)

if [ "$OS" = "Darwin" ]; then 
  export RSADIR=/usr/local/etc/pki
  export EASYRSA_CMD=easyrsa
else
  export RSADIR=$(pwd)/easy-rsa
  export EASYRSA_CMD=$RSADIR/easysa
fi

if [ "$OS" = "Darwin" ]; then
  if [ ! "$TERRAFORM_INSTALLED" ]; then
    brew tap hashicorp/tap
    brew install hashicorp/tap/terraform
  fi
  if [ ! "$EASYRSA_INSTALLED" ]; then
    brew install easy-rsa
  fi
  if [ ! "$OPENVPN_INSTALLED" ]; then
    brew install openvpn
  fi
fi

if [ "$OS" = "Linux" ]; then
  if [ ! = "$TERRAFORM_INSTALLED" ]; then
    if [ -f /etc/debian_version ]; then
      wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor | sudo tee /usr/share/keyrings/hashicorp-archive-keyring.gpg
      echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
      sudo apt update && sudo apt install terraform -y
    fi
  fi
  if [ ! "$EASYRSA_INSTALLED" ]; then
    apt install easy-rsa -y
  fi
  if [ ! "$OPENVPN_INSTALLED" ]; then
    apt install openvpn -y
  fi 
fi

if [ ! "$OS" = "Darwin" ]; then 
    mkdir $RSADIR
    ln -s /usr/share/easy-rsa/* $RSADIR/
    cd $RSADIR
    cp vars.example vars
    cd $RSADIR
    export REAL_PKI=$RSADIR/pki
fi

if [ "$OS" == "Darwin" ]; then
  mkdir ./easy-rsa
  cd ./easy-rsa
  export REAL_PKI=/usr/local/etc/pki
fi

### Initial Copies are just for Backup Purposes

$EASYRSA_CMD --batch init-pki
$EASYRSA_CMD --batch build-ca nopass
$EASYRSA_CMD --batch gen-req server nopass
$EASYRSA_CMD --batch sign-req server server
mkdir -p $RSADIR/server-configs/keys
mkdir -p $RSADIR/client-configs/keys
openvpn --genkey secret $RSADIR/ta.key
/usr/bin/openssl dhparam -out $RSADIR/dh2048.pem 2048
cp -v $REAL_PKI/private/server.key $RSADIR/server-configs/keys/
cp -v $REAL_PKI/issued/server.crt $REAL_PKI/ca.crt $RSADIR/server-configs/keys/
cp -v $REAL_PKI/ca.crt $RSADIR/server-configs/keys/
cp -v $REAL_PKI/ca.crt $RSADIR/client-configs/keys/
cp -v $RSADIR/ta.key $REAL_PKI/ca.crt $RSADIR/server-configs/keys/
cp -v $RSADIR/dh2048.pem $RSADIR/server-configs/keys/
$EASYRSA_CMD --batch gen-req client1 nopass
$EASYRSA_CMD --batch sign-req client client1
cp -v $REAL_PKI/private/client1.key $RSADIR/client-configs/keys/
cp -v $REAL_PKI/issued/client1.crt $RSADIR/client-configs/keys/

### Getting ready for Ansible 

cp -v $REAL_PKI/private/* $CURDIR/ansible/roles/openvpn_server/files/
cp -v $REAL_PKI/issued/* $CURDIR/ansible/roles/openvpn_server/files/
cp -v $RSADIR/server-configs/keys/* $CURDIR/ansible/roles/openvpn_server/files/
cp -v $RSADIR/client-configs/keys/*.crt $CURDIR/ansible/roles/openvpn_server/files/
cp -v $RSADIR/client-configs/keys/*.key $CURDIR/ansible/roles/openvpn_server/files/

#cp /usr/share/doc/openvpn/examples/sample-config-files/server.conf /etc/openvpn/server/
#sed -i 's/tls-auth ta.key 0/;tls-auth ta.key 0\ntls-crypt ta.key/g' /etc/openvpn/server/server.conf
#sed -i 's/cipher AES-256-CBC/;cipher AES-256-CBC\ncipher AES-256-GCM\nauth SHA256/g' /etc/openvpn/server/server.conf
#sed -i 's/;user nobody/user nobody/g' /etc/openvpn/server/server.conf
#sed -i 's/;group nobody/group nobody/g' /etc/openvpn/server/server.conf
#echo "" >> /etc/openvpn/server/server.conf
#echo 'push "redirect-gateway def1 bypass-dhcp"' >> /etc/openvpn/server/server.conf
#echo 'push "dhcp-option DNS 208.67.222.222"' >> /etc/openvpn/server/server.conf
#echo 'push "dhcp-option DNS 208.67.220.220"' >> /etc/openvpn/server/server.conf
#sed -i 's/#net.ipv4.ip_forward=1/net.ipv4.ip_forward=1/g' /etc/sysctl.conf
#$EASYRSA_CMD --batch gen-req node1 nopass
#$EASYRSA_CMD --batch sign-req client node1
#cp pki/private/node1.key /root/client-configs/keys/
#cp pki/issued/node1.crt /root/client-configs/keys/ 
#systemctl -f enable openvpn-server@server.service
#systemctl start openvpn-server@server.service
#sysctl -w net.ipv4.ip_forward=1





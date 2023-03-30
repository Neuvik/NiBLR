#!/bin/bash

set -eu

export TERRAFORM_VERSION="1.4.2"
export OS="$(uname)"
export TERRAFORM_INSTALLED="$(which terraform)"
#export EASYRSA_INSTALLED="$(which easyrsa)"
#export OPENVPN_INSTALLED="$(which openvpn)"
export CURDIR=$(pwd)
export WIREGUARD_TOOLS=$(which wg)

#if [ "$OS" = "Darwin" ]; then 
  #export RSADIR=/usr/local/etc/pki
  #export EASYRSA_CMD=easyrsa
#else
  #export RSADIR=$(pwd)/easy-rsa
  #export EASYRSA_CMD=$RSADIR/easysa
#fi

if [ "$OS" = "Darwin" ]; then
  if [ ! "$TERRAFORM_INSTALLED" ]; then
    brew tap hashicorp/tap
    brew install hashicorp/tap/terraform
  fi
  if [ ! "$WIREGUARD_TOOLS" ]; then
    brew install wireguard-tools
  fi
  #if [ ! "$EASYRSA_INSTALLED" ]; then
  #  brew install easy-rsa
  #fi
  #if [ ! "$OPENVPN_INSTALLED" ]; then
  #  brew install openvpn
  #fi
fi

if [ "$OS" = "Linux" ]; then
  if [ ! = "$TERRAFORM_INSTALLED" ]; then
    if [ -f /etc/debian_version ]; then
      wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor | sudo tee /usr/share/keyrings/hashicorp-archive-keyring.gpg
      echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
      sudo apt update && sudo apt install terraform -y
    fi
  fi
  if [ ! "$WIREGUARD_TOOLS "]; then
    apt install wireguard -y
  fi
  #if [ ! "$EASYRSA_INSTALLED" ]; then
  #  apt install easy-rsa -y
  #fi
  #if [ ! "$OPENVPN_INSTALLED" ]; then
  #  apt install openvpn -y
  #fi 
fi

#if [ ! "$OS" = "Darwin" ]; then 
    #mkdir $RSADIR
    #ln -s /usr/share/easy-rsa/* $RSADIR/
    #cd $RSADIR
    #cp vars.example vars
    #cd $RSADIR
    #export REAL_PKI=$RSADIR/pki
#fi

#if [ "$OS" == "Darwin" ]; then
#  mkdir ./easy-rsa
#  cd ./easy-rsa
#  export REAL_PKI=/usr/local/etc/pki
#fi

# setup initial wireguard configurations

umask 077
if [ ! -d "$CURDIR/wireguard_configs" ]; then
  mkdir -p $CURDIR/wireguard_configs
fi

NUM=${1?Need the number of exit nodes, default is 2}

cd $CURDIR/wireguard_configs

if [ ! -f $CURDIR/wireguard_configs/wgHub.pub ]; then
  echo "Building the wgHub.key"
  wg genkey | sudo tee wgHub.key | wg pubkey | sudo tee wgHub.pub
fi

if [ ! -f $CURDIR/wireguard_configs/client1.pub ]; then
   echo "Building the client1.key"
  wg genkey | sudo tee client1.key | wg pubkey | sudo tee client1.pub
fi

if [ ! -f $CURDIR/wireguard_configs/exit-hub$NUM.pub ]; then
    echo "Building Exit Node Keys for $NUM hosts"
    (( ++NUM ))
    while (( --NUM >= 1 ))
    do 
      wg genkey | sudo tee exit-hub$NUM.key | wg pubkey | sudo tee exit-hub$NUM.pub
    done
fi
sudo chown -R $(id -u):$(id -g) .




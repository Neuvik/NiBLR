#!/bin/bash

set -eu

export OS="$(uname)"
export CURDIR=$(pwd)

#if [ "$OS" = "Darwin" ]; then 
#  export RSADIR=/usr/local/etc/pki
#  export EASYRSA_CMD=easyrsa
#else
#  export RSADIR=$(pwd)/easy-rsa
#  export EASYRSA_CMD=$RSADIR/easysa
#fi

#if [ ! "$OS" = "Darwin" ]; then 
#    export REAL_PKI=$RSADIR/pki
#fi
#
#if [ "$OS" = "Darwin" ]; then
#  export REAL_PKI=/usr/local/etc/pki
#fi

NODE_NUM=$1
if [ ! -f "wireguard_config/wgNode$NODE_NUM." ]; then
  $EASYRSA_CMD --batch gen-req node$NODE_NUM nopass
  $EASYRSA_CMD --batch sign-req client node$NODE_NUM
  cp -v $RSADIR/issued/node$NODE_NUM.crt ansible/roles/exit_nodes/files/node$NODE_NUM.crt
  cp -v $RSADIR/private/node$NODE_NUM.key ansible/roles/exit_nodes/files/node$NODE_NUM.key
  cp -v $RSADIR/issued/node$NODE_NUM.crt ansible/roles/openvpn_server/files/node$NODE_NUM.crt
  cp -v $RSADIR/private/node$NODE_NUM.key ansible/roles/openvpn_server/files/node$NODE_NUM.key
  exit 0
else
  echo "This node exists"
  exit 0
fi


#!/bin/bash

# This file will delete all existing PKI. Use with caution

OS=$(uname)
CURDIR=$(pwd)

if [ "$OS" == "Darwin" ]; then
  rm -Rf ./easy-rsa
  rm -Rf /usr/local/etc/pki/*
fi

if [ "$OS" == "Linux" ]; then
  rm -Rf $CURDIR/easy-rsa/pki/*
  rm -Rf $CURDIR/client-configs
fi
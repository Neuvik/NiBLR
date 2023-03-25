#!/bin/bash

OS=$(uname -a)

if [ "$OS" == "Darwin" ]; then
   
fi
cd terraform
terraform apply -auto-approve

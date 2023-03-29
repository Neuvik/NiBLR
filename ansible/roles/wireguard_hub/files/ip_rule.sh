#!/bin/bash
TEST=$(ip rule | grep 10.10.9.0)

if [ -z "$TEST" ]; then
    ip rule add from 10.10.9.0/24 table loadb
    echo "Added Rule!"
    exit 0 
else
    echo "Rule Exists!"
    exit 0
fi


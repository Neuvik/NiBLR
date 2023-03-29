#!/bin/bash

EXIT_CONFS=$(cd /etc/wireguard; ls exit-node*| sed -e 's/.conf//')
for INT in $EXIT_CONFS
  do
    echo $INT
    CHECK=$(ip addr show $INT)
    if [ "$CHECK" == "Device \"INT\" does not exist." ]
    echo "This is: $CHECK"
    then
        cd /etc/wireguard
        systemctl enable wg-quick@$INT.service
	sudo systemctl daemon-reload
        systemctl start wg-quick@$INT.service
    fi
  done
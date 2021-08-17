#!/bin/sh
pmc -u -f /var/run/$1 'GET PORT_DATA_SET' | gawk '/portState/ { print $2; }'




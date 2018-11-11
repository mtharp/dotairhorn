#!/bin/bash
cd /a
chown -R dotairhorn:dotairhorn /a
exec /usr/local/bin/gosu dotairhorn "$@"

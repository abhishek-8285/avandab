#!/bin/sh
# Correct launch for opencode on Avandab server - prevents root scan
export HOME=/root
export PWD=/avandab
export NODE_OPTIONS="--max-old-space-size=512"
cd /avandab
exec /.opencode/bin/opencode "$@"

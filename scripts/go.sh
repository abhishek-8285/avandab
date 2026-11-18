#!/bin/sh
# Locate the Go toolchain and exec it with the given arguments.
#
# Some runners (CI sandboxes, Craft Anything checks) call `npm test` with a
# minimal PATH that omits the Go install directory. Keep the npm scripts
# portable by resolving go here instead of hardcoding it in package.json.
set -e

if command -v go >/dev/null 2>&1; then
    exec go "$@"
fi

for candidate in \
    "${GOROOT:-/nonexistent}/bin/go" \
    /home/abhishek/go/bin/go \
    /usr/local/go/bin/go \
    /usr/lib/go/bin/go \
    /opt/go/bin/go
do
    if [ -x "$candidate" ]; then
        exec "$candidate" "$@"
    fi
done

echo "go.sh: Go toolchain not found on PATH or in the standard locations" >&2
exit 127

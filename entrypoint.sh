#!/bin/sh
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

exec su-exec "$PUID:$PGID" "$@"

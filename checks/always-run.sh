#!/usr/bin/env sh
set -eu

printf '{"run":true,"reason":"demo check always runs","event_id":"demo:%s"}\n' "$(date -u +%Y%m%dT%H%M%SZ)"

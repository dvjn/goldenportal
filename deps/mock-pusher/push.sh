#!/bin/sh
# Pushes the example payloads to the seeded push integrations on a loop, as an
# external system would. Each round "deploys" one random service, so runs show
# updates as well as unchanged entities. billing-export is invalid on purpose,
# and legacy-reports is filtered out by the mapping.
set -u

api="${GP_API_URL:-http://localhost:47200}"
interval="${PUSH_INTERVAL:-60}"
state=/tmp/deploys
mkdir -p "$state"

timestamp() { date -u -d "@$1" +%Y-%m-%dT%H:%M:%SZ; }

services=$(grep -o '@DEPLOY:[a-z-]*@' /data/service-registry.json | sed 's/@DEPLOY:\(.*\)@/\1/')
hours=3
for service in $services; do
  timestamp $(($(date +%s) - hours * 3600)) >"$state/$service"
  hours=$((hours + 7))
done

push() {
  status=$(curl -sS -o /tmp/response -w '%{http_code}' -X POST \
    -H "Authorization: Bearer $2" -H 'Content-Type: application/json' \
    --data-binary @"$3" "$api/api/v1/ingest/$1") || status=000
  echo "$(timestamp "$(date +%s)") $1: HTTP $status $(head -c 300 /tmp/response 2>/dev/null)"
}

while true; do
  count=$(echo "$services" | wc -w)
  deployed=$(echo $services | cut -d' ' -f$((RANDOM % count + 1)))
  timestamp "$(date +%s)" >"$state/$deployed"

  cp /data/service-registry.json /tmp/service-registry.json
  for service in $services; do
    sed -i "s/@DEPLOY:$service@/$(cat "$state/$service")/" /tmp/service-registry.json
  done

  push team-directory "$TEAM_DIRECTORY_TOKEN" /data/team-directory.json
  push service-registry "$SERVICE_REGISTRY_TOKEN" /tmp/service-registry.json
  sleep "$interval"
done

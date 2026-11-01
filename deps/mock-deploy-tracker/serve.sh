#!/bin/sh
# A pretend deploy tracker for the http connector to pull from. It serves the
# latest deployments as two JSON pages, /deployments/1.json linking to
# 2.json, and every TICK seconds finishes the running deployment and starts
# a new one. Only the latest 20 are kept, so older ones drop out of syncs.
set -u

tick="${TICK:-30}"
www=/tmp/www
state=/tmp/deployments
releases=/tmp/releases
mkdir -p "$www/deployments" "$releases"
: >"$state"

services="payments-api ledger checkout-web search-api indexer auth-service postgres-proxy api-gateway feature-flags"
environments="production production staging"

timestamp() { date -u -d "@$1" +%Y-%m-%dT%H:%M:%SZ; }
# pick sets picked to one of a list's words at random. It runs in this shell,
# not a subshell, so RANDOM moves on between picks instead of repeating.
pick() {
  set -- $1
  shift $((RANDOM % $#))
  picked=$1
}

# deploy appends a deployment: id|service|environment|version|status|started|finished
# Each service counts its own releases, so versions read like semver.
deploy() {
  id=$1 started=$2 status=$3 finished=$4
  pick "$services"
  service=$picked
  release=$(($(cat "$releases/$service" 2>/dev/null || echo 20) + 1))
  echo "$release" >"$releases/$service"
  version="1.$((release / 10)).$((release % 10))"
  pick "$environments"
  echo "$id|$service|$picked|$version|$status|$started|$finished" >>"$state"
}

# finish marks running deployments as done, one in five failed.
finish() {
  now=$(timestamp "$(date +%s)")
  awk -F'|' -v OFS='|' -v now="$now" -v roll="$RANDOM" '
    $5 == "running" { $5 = (roll % 5 == 0) ? "failed" : "succeeded"; $7 = now }
    { print }' "$state" >"$state.next" && mv "$state.next" "$state"
}

# render writes the newest 20 deployments as two pages of 10, newest first.
render() {
  tail -n 20 "$state" >"$state.next" && mv "$state.next" "$state"
  sort -t'|' -k1,1nr "$state" | awk -F'|' -v dir="$www/deployments" '
    function page(n) { return dir "/" n ".json" }
    function close_page(n, next_link) {
      printf "\n  ],\n  \"next\": %s\n}\n", next_link > page(n)
      close(page(n))
    }
    {
      n = int((NR - 1) / 10) + 1
      if ((NR - 1) % 10 == 0) {
        if (n > 1) close_page(n - 1, "\"" n ".json\"")
        printf "{\n  \"deployments\": [\n" > page(n)
      } else {
        printf ",\n" > page(n)
      }
      finished = ($7 == "") ? "null" : "\"" $7 "\""
      printf "    {\"id\": \"deploy-%s\", \"service\": \"%s\", \"environment\": \"%s\", \"version\": \"%s\", \"status\": \"%s\", \"startedAt\": \"%s\", \"finishedAt\": %s}", $1, $2, $3, $4, $5, $6, finished > page(n)
      last = n
    }
    END { if (last) close_page(last, "null") }'
}

# Start with a history of finished deployments, an hour apart, a few failed.
id=1
start=$(($(date +%s) - 15 * 3600))
while [ "$id" -le 15 ]; do
  at=$((start + (id - 1) * 3600))
  status=succeeded
  [ $((id % 6)) -eq 0 ] && status=failed
  deploy "$id" "$(timestamp "$at")" "$status" "$(timestamp $((at + 300)))"
  id=$((id + 1))
done
render

httpd -f -p 8080 -h "$www" &

while true; do
  sleep "$tick"
  finish
  deploy "$id" "$(timestamp "$(date +%s)")" running ""
  id=$((id + 1))
  render
done

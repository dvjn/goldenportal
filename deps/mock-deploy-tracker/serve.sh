#!/bin/sh
# A pretend deploy tracker for the http connector to pull from. It serves the
# latest deployments as two JSON pages, /deployments/1.json linking to
# 2.json, and every TICK seconds finishes the running deployment and starts
# a new one. Only the latest 20 are kept, so older ones drop out of syncs.
set -u

tick="${TICK:-30}"
www=/tmp/www
state=/tmp/deployments
mkdir -p "$www/deployments"
: >"$state"

services="payments-api ledger checkout-web search-api indexer auth-service postgres-proxy"
environments="production production staging"

timestamp() { date -u -d "@$1" +%Y-%m-%dT%H:%M:%SZ; }
pick() { echo $1 | tr ' ' '\n' | awk -v n="$RANDOM" 'NR > 0 { items[NR] = $0 } END { print items[n % NR + 1] }'; }

# deploy appends a deployment: id|service|environment|version|status|started|finished
deploy() {
  id=$1 started=$2 status=$3 finished=$4
  echo "$id|$(pick "$services")|$(pick "$environments")|1.$id.0|$status|$started|$finished" >>"$state"
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

# Start with a history of finished deployments, an hour apart.
id=1
start=$(($(date +%s) - 15 * 3600))
while [ "$id" -le 15 ]; do
  at=$((start + (id - 1) * 3600))
  deploy "$id" "$(timestamp "$at")" succeeded "$(timestamp $((at + 300)))"
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

#!/usr/bin/env bash
# Supplied tool. It checks real requests, not a learner-written passed=true.
set -euo pipefail
base=${BASE:?Set BASE to your own TicketLab instance}
root=$(mktemp -d)
trap 'rm -f "$root/ready.json" "$root/create.json" "$root/read.json" "$root/invalid.json"; rmdir "$root"' EXIT
title="v5-check-$RANDOM-$$"
curl -fsS --max-time 5 "$base/ready" > "$root/ready.json"
code=$(curl -sS --max-time 5 -o "$root/create.json" -w '%{http_code}' -H 'Content-Type: application/json' -d "{\"title\":\"$title\"}" "$base/tickets")
test "$code" = 201
id=$(jq -er '.id' "$root/create.json")
code=$(curl -sS --max-time 5 -o "$root/read.json" -w '%{http_code}' "$base/tickets/$id")
test "$code" = 200
jq -e --arg id "$id" --arg title "$title" '.id == $id and .title == $title and .status == "open"' "$root/read.json"
code=$(curl -sS --max-time 5 -o "$root/invalid.json" -w '%{http_code}' -H 'Content-Type: application/json' -d '{"title":""}' "$base/tickets")
test "$code" = 400
printf '%s\n' 'PASS: ready, POST 201, separate GET 200 with matching fields, invalid input 400'

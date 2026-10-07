#!/usr/bin/env bash
set -euo pipefail
root=$(mktemp -d)
good_pid=''
bad_pid=''
cleanup() {
  [[ -z "$good_pid" ]] || kill "$good_pid" 2>/dev/null || true
  [[ -z "$bad_pid" ]] || kill "$bad_pid" 2>/dev/null || true
}
trap cleanup EXIT
mkdir -p reports
start_good() {
  ./dist/ticketlab serve --listen 127.0.0.1:18731 --data-dir "$root/good" --instance ci-good > reports/good.log 2>&1 &
  good_pid=$!
  for i in {1..50}; do
    if curl -fsS --max-time 1 http://127.0.0.1:18731/ready > reports/ready.json; then return; fi
    sleep .1
  done
  return 1
}
start_good
curl -fsS http://127.0.0.1:18731/version > reports/version.json
jq -e --arg sha "$GITHUB_SHA" '.source_revision == $sha and .instance_id == "ci-good"' reports/version.json
code=$(curl -sS -o reports/created.json -w '%{http_code}' -H 'Content-Type: application/json' -H 'Idempotency-Key: ci-one' -d '{"title":"CI сохраняет заявку"}' http://127.0.0.1:18731/tickets)
test "$code" = 201
id=$(jq -r .id reports/created.json)
curl -fsS "http://127.0.0.1:18731/tickets/$id" | jq -e '.title == "Намеренно неверное ожидание" and .status == "open"'
code=$(curl -sS -o reports/retry.json -w '%{http_code}' -H 'Content-Type: application/json' -H 'Idempotency-Key: ci-one' -d '{"title":"CI сохраняет заявку"}' http://127.0.0.1:18731/tickets)
test "$code" = 200
jq -e --arg id "$id" '.id == $id' reports/retry.json
code=$(curl -sS -o reports/invalid.json -w '%{http_code}' -H 'Content-Type: application/json' -d '{"title":""}' http://127.0.0.1:18731/tickets)
test "$code" = 400
kill "$good_pid"
wait "$good_pid"
good_pid=''
start_good
curl -fsS "http://127.0.0.1:18731/tickets/$id" | jq -e '.title == "CI сохраняет заявку"'
./dist/ticketlab-bad-write serve --listen 127.0.0.1:18732 --data-dir "$root/bad" --instance ci-bad > reports/bad.log 2>&1 &
bad_pid=$!
for i in {1..50}; do
  if curl -fsS --max-time 1 http://127.0.0.1:18732/ready > reports/bad-ready.json; then break; fi
  sleep .1
done
jq -e '.status == "ready"' reports/bad-ready.json
code=$(curl -sS -o reports/bad-write.json -w '%{http_code}' -H 'Content-Type: application/json' -d '{"title":"Это должно отказать"}' http://127.0.0.1:18732/tickets)
test "$code" = 503
jq -e '.error == "storage_unavailable"' reports/bad-write.json
printf '%s\n' 'PASS: POST/GET, validation, retry, restart persistence, known bad-write detected' | tee reports/http-result.txt

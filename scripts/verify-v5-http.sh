#!/usr/bin/env bash
set -euo pipefail
root=$(mktemp -d)
pid=''
cleanup() { [[ -z "$pid" ]] || kill "$pid" 2>/dev/null || true; }
trap cleanup EXIT
mkdir -p reports
port=${V5_HTTP_TEST_PORT:-18734}
for variant in good bad-write bad-body; do
  binary=./dist/ticketlab
  [[ "$variant" == good ]] || binary="./dist/ticketlab-$variant"
  "$binary" serve --listen "127.0.0.1:$port" --data-dir "$root/$variant" --instance "v5-$variant" > "reports/v5-$variant.log" 2>&1 &
  pid=$!
  ready=false
  for i in {1..50}; do
    if curl -fsS --max-time 1 "http://127.0.0.1:$port/ready" > /dev/null; then ready=true; break; fi
    sleep .1
  done
  "$ready"
  rc=0
  BASE="http://127.0.0.1:$port" bash practice/v5/check-http.sh > "reports/v5-$variant-check.txt" 2>&1 || rc=$?
  if [[ "$variant" == good ]]; then test "$rc" = 0; else test "$rc" != 0; fi
  printf 'VERIFIED HTTP %s checker_exit=%s\n' "$variant" "$rc"
  kill "$pid"
  wait "$pid" || true
  pid=''
done
rc=0
BASE="http://127.0.0.1:$port" bash practice/v5/check-http.sh > reports/v5-unavailable-check.txt 2>&1 || rc=$?
test "$rc" != 0
printf 'VERIFIED HTTP unavailable checker_exit=%s\n' "$rc"
printf '%s\n' 'PASS: supplied checker accepts working API and rejects bad-write, wrong body and unavailable API'

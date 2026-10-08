#!/usr/bin/env bash
set -euo pipefail
name="peaky-ci-${GITHUB_RUN_ID:-local-$$}"
image_ref="${IMAGE_REF:-ticketlab:check}"
test_port="${CONTAINER_TEST_PORT:-18733}"
mkdir -p reports
volume="$name-data"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT
docker volume create "$volume" >/dev/null
run() {
  docker run -d --name "$name" -p "127.0.0.1:$test_port:8080" -v "$volume:/data" "$image_ref"
  for i in {1..50}; do
    if curl -fsS --max-time 1 "http://127.0.0.1:$test_port/ready" > reports/container-ready.json; then return; fi
    sleep .2
  done
  docker logs "$name"
  return 1
}
run
docker inspect "$name" --format '{{.Image}}' > reports/runtime-image-id.txt
docker image inspect "$image_ref" --format '{{.Id}}' > reports/built-image-id.txt
cmp reports/runtime-image-id.txt reports/built-image-id.txt
curl -fsS -H 'Content-Type: application/json' -d '{"title":"Заявка переживает recreate"}' "http://127.0.0.1:$test_port/tickets" > reports/container-created.json
id=$(jq -r .id reports/container-created.json)
docker rm -f "$name"
run
curl -fsS "http://127.0.0.1:$test_port/tickets/$id" | jq -e '.title == "Заявка переживает recreate"'
docker logs "$name" > reports/container.log 2>&1
printf '%s\n' 'PASS: actual image and persistence after recreate' | tee reports/container-result.txt

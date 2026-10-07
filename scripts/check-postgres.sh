#!/usr/bin/env bash
set -euo pipefail
export COMPOSE_PROJECT_NAME="peaky-restore-${GITHUB_RUN_ID:-local}"
export TICKETLAB_PORT=18983
mkdir -p reports
work=$(mktemp -d)
cleanup() {
  docker compose -p "$COMPOSE_PROJECT_NAME" logs --no-color > reports/postgres-compose.log 2>&1 || true
  docker compose -p "$COMPOSE_PROJECT_NAME" down -v --remove-orphans > "$work/cleanup.log" 2>&1 || cat "$work/cleanup.log"
  rm -rf -- "$work"
}
trap cleanup EXIT
dc() { docker compose -p "$COMPOSE_PROJECT_NAME" "$@"; }
dc up -d
for n in {1..60}; do
  if curl -fsS "http://127.0.0.1:$TICKETLAB_PORT/ready" > /dev/null; then break; fi
  sleep 1
done
curl -fsS -H 'Content-Type: application/json' -d '{"title":"Данные до резервной копии"}' "http://127.0.0.1:$TICKETLAB_PORT/tickets" > "$work/created.json"
ticket_id=$(jq -er .id "$work/created.json")
dc exec -T db pg_dump -U student -d ticketlab -Fc > "$work/ticketlab.dump"
test -s "$work/ticketlab.dump"
dc exec -T db createdb -U student ticketlab_restore
dc exec -T db pg_restore -U student -d ticketlab_restore --exit-on-error < "$work/ticketlab.dump"
dc run --rm --no-deps -d --name "$COMPOSE_PROJECT_NAME-restored-api" -p 127.0.0.1:18984:8080 -e TICKETLAB_DATABASE_URL=postgres://student:training-only-change-for-real-use@db:5432/ticketlab_restore?sslmode=disable api
for n in {1..60}; do
  if curl -fsS http://127.0.0.1:18984/ready > /dev/null; then break; fi
  sleep 1
done
curl -fsS "http://127.0.0.1:18984/tickets/$ticket_id" | jq -e '.title == "Данные до резервной копии" and .status == "open"'
# Restore must reject a corrupt file, not merely exist as a command in the course.
printf 'not a PostgreSQL archive\n' > "$work/corrupt.dump"
if dc exec -T db pg_restore -U student -d ticketlab_restore --exit-on-error < "$work/corrupt.dump"; then
  echo 'Corrupt archive was incorrectly accepted' >&2
  exit 1
fi
curl -fsS "http://127.0.0.1:$TICKETLAB_PORT/tickets/$ticket_id" | jq -e '.title == "Данные до резервной копии"'
printf 'PASS custom-format backup\nPASS restore into separate database\nPASS restored application reads original ID and title\nPASS corrupt archive rejected\nPASS original application still reads original data\n' > reports/postgres-restore.txt
cat reports/postgres-restore.txt

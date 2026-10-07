#!/bin/bash
set -euo pipefail
full_run=${1:?unique full run ID}
full_kit=${2:?installed student archive directory}
mkdir -m700 -p "$HOME/peaky-labs/$full_run"
cd "$full_kit"
sha256sum -c SHA256SUMS
./ticketlab version
./labcheck doctor --module 1 --workspace "$HOME/peaky-labs/$full_run"
#!/bin/bash
set -euo pipefail
export PATH="$full_kit:$PATH"
run_id="$full_run/early"
root="$HOME/peaky-labs/$run_id"
mkdir -m 700 -p "$root"
result="$root/results.tsv"
printf 'case\texpected_exit\tactual_exit\n' > "$result"
case_run() {
  local name=$1 expected=$2
  shift 2
  printf '\nCOMMAND %s: ' "$name"
  printf '%q ' "$@"
  printf '\n'
  set +e
  "$@" < /dev/null > "$root/$name.console.txt" 2>&1
  local actual=$?
  set -e
  cat "$root/$name.console.txt"
  printf '%s\t%s\t%s\n' "$name" "$expected" "$actual" >> "$result"
  if [[ $actual != "$expected" ]]; then
    printf 'HARNESS FAILURE %s expected=%s actual=%s\n' "$name" "$expected" "$actual" >&2
    exit 90
  fi
}
check() {
  local case_name=$1 expected=$2 profile=$3 id=$4
  shift 4
  case_run "$case_name" "$expected" labcheck check "$profile" --seed 418 --instance "$id" --workspace "$root/$id" --report "$root/$case_name.json" "$@"
}
case_run doctor 0 labcheck doctor --workspace "$root" --module 6
case_run m01-prepare 0 labctl prepare m01 --seed 418 --instance files-a-v4a --root "$root"
check m01-initial-fails 1 m01-files files-a-v4a
cp "$root/files-a-v4a/inputs/отчёт смены.txt" "$root/files-a-v4a/evidence/отчёт смены.txt"
grep 'ticket=T418' "$root/files-a-v4a/inputs/service.log" > "$root/files-a-v4a/evidence/T418.log"
check m01-correct 0 m01-files files-a-v4a
printf '2026-10-04T10:00:01Z ERROR ticket=T418 write_failed\n' > "$root/files-a-v4a/evidence/T418.log"
check m01-incomplete-log-fails 1 m01-files files-a-v4a
grep 'ticket=T418' "$root/files-a-v4a/inputs/service.log" > "$root/files-a-v4a/evidence/T418.log"
check m01-fixed 0 m01-files files-a-v4a
mv "$root/files-a-v4a/evidence/T418.log" "$root/files-a-v4a/evidence/T417.log"
check m01-neighbour-seed-fails 1 m01-files files-a-v4a
mv "$root/files-a-v4a/evidence/T417.log" "$root/files-a-v4a/evidence/T418.log"
check m01-seed-fixed 0 m01-files files-a-v4a

case_run m02-prepare 0 labctl prepare m02 --seed 418 --instance linux-a-v4a --root "$root" --port 21002
case_run peer-prepare 0 labctl prepare m02 --seed 418 --instance linux-b-v4a --root "$root" --port 21202
case_run m02-start 0 labctl start --instance linux-a-v4a --root "$root"
case_run peer-start 0 labctl start --instance linux-b-v4a --root "$root"
check m02-permissions-fail 1 m02-linux linux-a-v4a --peer-workspace "$root/linux-b-v4a" --peer-instance linux-b-v4a
chmod u+r "$root/linux-a-v4a/inputs/only-owner.txt"
check m02-correct 0 m02-linux linux-a-v4a --peer-workspace "$root/linux-b-v4a" --peer-instance linux-b-v4a
chmod 0666 "$root/linux-a-v4a/inputs/only-owner.txt"
check m02-world-write-fails 1 m02-linux linux-a-v4a --peer-workspace "$root/linux-b-v4a" --peer-instance linux-b-v4a
chmod go-rw "$root/linux-a-v4a/inputs/only-owner.txt"
check m02-alternative-fix 0 m02-linux linux-a-v4a --peer-workspace "$root/linux-b-v4a" --peer-instance linux-b-v4a
case_run m02-stop 0 labctl stop --instance linux-a-v4a --root "$root"
check m02-stopped-fails 1 m02-linux linux-a-v4a --peer-workspace "$root/linux-b-v4a" --peer-instance linux-b-v4a
case_run m02-restart 0 labctl start --instance linux-a-v4a --root "$root"
check m02-after-restart 0 m02-linux linux-a-v4a --peer-workspace "$root/linux-b-v4a" --peer-instance linux-b-v4a

case_run m03-prepare 0 labctl prepare m03 --seed 418 --instance http-a-v4a --root "$root" --port 21003
case_run m03-start 0 labctl start --instance http-a-v4a --root "$root"
check m03-correct 0 m03-http http-a-v4a --base-url http://127.0.0.1:21003
case_run m03-stop 0 labctl stop --instance http-a-v4a --root "$root"
"$full_kit"/ticketlab-bad-body serve --listen 127.0.0.1:21003 --data-dir "$root/http-a-v4a/data" --instance http-a-v4a > "$root/http-bad.log" 2>&1 &
bad_pid=$!
for n in {1..30}; do if curl -sf http://127.0.0.1:21003/version > /dev/null; then break; fi; sleep 1; done
check m03-wrong-body-fails 1 m03-http http-a-v4a --base-url http://127.0.0.1:21003 --expected-release 3.0.0-bad-body
kill -TERM "$bad_pid"
wait "$bad_pid"
case_run m03-fixed-start 0 labctl start --instance http-a-v4a --root "$root"
check m03-fixed 0 m03-http http-a-v4a --base-url http://127.0.0.1:21003

case_run m04-prepare 0 labctl prepare m04 --seed 418 --instance terminal-a-v4a --root "$root"
check m04-initial-fails 1 m04-terminal terminal-a-v4a
case_run m04-command-fails 1 bash --noprofile --norc -c 'labcheck intentional-failure > "$1/evidence/stdout.txt" 2> "$1/evidence/stderr.txt"' route "$root/terminal-a-v4a"
sort -u "$root/terminal-a-v4a/inputs/codes.log" > "$root/terminal-a-v4a/evidence/codes.txt"
check m04-hidden-pipeline-fails 1 m04-terminal terminal-a-v4a
printf 'pipefail=on\n' > "$root/terminal-a-v4a/config/pipeline.env"
check m04-correct 0 m04-terminal terminal-a-v4a
printf 'wrong\n' > "$root/terminal-a-v4a/evidence/stderr.txt"
check m04-stderr-fails 1 m04-terminal terminal-a-v4a
case_run m04-repeat-command 1 bash --noprofile --norc -c 'labcheck intentional-failure > "$1/evidence/stdout.txt" 2> "$1/evidence/stderr.txt"' route "$root/terminal-a-v4a"
check m04-fixed 0 m04-terminal terminal-a-v4a

case_run m05-prepare 0 labctl prepare m05 --seed 418 --instance git-a-v4a --root "$root"
check m05-initial-fails 1 m05-git git-a-v4a
printf 'TicketLab\nport=18005\n' > "$root/git-a-v4a/repo/README.txt"
git -C "$root/git-a-v4a/repo" add README.txt
git -C "$root/git-a-v4a/repo" commit -m 'Исправлен порт сервиса'
check m05-correct 0 m05-git git-a-v4a
git -C "$root/git-a-v4a/repo" add draft.txt
git -C "$root/git-a-v4a/repo" commit -m 'Неверно добавлен черновик'
check m05-draft-fails 1 m05-git git-a-v4a
git -C "$root/git-a-v4a/repo" rm --cached draft.txt
git -C "$root/git-a-v4a/repo" commit -m 'Черновик исключён из поставки'
check m05-fixed 0 m05-git git-a-v4a

case_run m06-prepare 0 labctl prepare m06 --seed 418 --instance service-a-v4a --root "$root" --port 21006
case_run m06-start 0 labctl start --instance service-a-v4a --root "$root"
baseline="$root/service-a-v4a/state/before.json"
case_run m06-baseline 0 labcheck baseline m06-service --seed 418 --instance service-a-v4a --base-url http://127.0.0.1:21006 --state "$baseline"
case_run m06-baseline-immutable 64 labcheck baseline m06-service --seed 418 --instance service-a-v4a --base-url http://127.0.0.1:21006 --state "$baseline"
check m06-correct 0 m06-service service-a-v4a --base-url http://127.0.0.1:21006 --state "$baseline"
case_run m06-stop 0 labctl stop --instance service-a-v4a --root "$root"
mv "$root/service-a-v4a/data/tickets.json" "$root/service-a-v4a/evidence/tickets.saved.json"
case_run m06-wrong-start 0 labctl start --instance service-a-v4a --root "$root"
check m06-old-data-fails 1 m06-service service-a-v4a --base-url http://127.0.0.1:21006 --state "$baseline"
case_run m06-wrong-stop 0 labctl stop --instance service-a-v4a --root "$root"
mv "$root/service-a-v4a/evidence/tickets.saved.json" "$root/service-a-v4a/data/tickets.json"
case_run m06-fixed-start 0 labctl start --instance service-a-v4a --root "$root"
check m06-fixed 0 m06-service service-a-v4a --base-url http://127.0.0.1:21006 --state "$baseline"
case_run m06-restart-stop 0 labctl stop --instance service-a-v4a --root "$root"
case_run m06-restart-start 0 labctl start --instance service-a-v4a --root "$root"
check m06-after-restart 0 m06-service service-a-v4a --base-url http://127.0.0.1:21006 --state "$baseline"
case_run reset-wrong-confirm 64 labctl reset --instance linux-a-v4a --root "$root" --confirm-instance linux-b-v4a
case_run reset-running-refused 64 labctl reset --instance linux-a-v4a --root "$root" --confirm-instance linux-a-v4a
case_run reset-own-stop 0 labctl stop --instance linux-a-v4a --root "$root"
case_run reset-own 0 labctl reset --instance linux-a-v4a --root "$root" --confirm-instance linux-a-v4a
case_run peer-survived 0 labctl status --instance linux-b-v4a --root "$root"
chmod u+r "$root/linux-b-v4a/inputs/only-owner.txt"
check peer-fully-healthy 0 m02-linux linux-b-v4a --peer-workspace "$root/http-a-v4a" --peer-instance http-a-v4a
for id in linux-b-v4a http-a-v4a service-a-v4a; do case_run "cleanup-$id" 0 labctl stop --instance "$id" --root "$root"; done
printf '\nROUTE_COMPLETE %s\n' "$root"
cat "$result"

#!/bin/bash
set -euo pipefail
export PATH="$full_kit:$PATH"
run_id="$full_run/containers"
start_module=7
root="$HOME/peaky-labs/$run_id"
mkdir -m700 -p "$root"
result="$root/results.tsv"
printf 'case\texpected_exit\tactual_exit\n' > "$result"
case_run() {
 local name=$1 expected=$2;shift 2
 printf '\nCOMMAND %s: ' "$name";printf '%q ' "$@";printf '\n'
 set +e
 "$@" < /dev/null > "$root/$name.console.txt" 2>&1
 local actual=$?
 set -e
 cat "$root/$name.console.txt"
 printf '%s\t%s\t%s\n' "$name" "$expected" "$actual" >> "$result"
 if [[ $actual != "$expected" ]];then printf 'HARNESS FAILURE %s expected=%s actual=%s\n' "$name" "$expected" "$actual" >&2;exit 90;fi
}
check() {
 local name=$1 expected=$2 profile=$3 id=$4;shift 4
 case_run "$name" "$expected" labcheck check "$profile" --seed 418 --instance "$id" --workspace "$root/$id" --report "$root/$name.json" --expected-source "$(jq -r '.source_revision' "$root/$id/state/release.json")" "$@"
}
wait_ready() {
 for n in {1..90};do if curl -sf --max-time 2 "$1/ready" > /dev/null;then return 0;fi;sleep 1;done
 return 2
}
compose() { docker compose --project-directory "$root/$1" -p "peaky301758-$1" "${@:2}"; }
image_id() { docker image inspect "peaky301758-$1:3.0.0" --format '{{.Id}}'; }
case_run doctor 0 labcheck doctor --workspace "$root" --module 8
if [[ $start_module -le 7 ]];then
case_run m07-prepare 0 labctl prepare m07 --seed 418 --instance container-a-v4a --root "$root" --port 21007
case_run m07-start 0 labctl start --instance container-a-v4a --root "$root"
case_run m07-ready 0 wait_ready http://127.0.0.1:21007
base="$root/container-a-v4a/state/before.json"
api=peaky301758-container-a-v4a-api-1
case_run m07-baseline 0 labcheck baseline m07-persistence --seed 418 --instance container-a-v4a --base-url http://127.0.0.1:21007 --container "$api" --state "$base"
check m07-no-recreate-fails 1 m07-persistence container-a-v4a --base-url http://127.0.0.1:21007 --container "$api" --expected-image "$(image_id container-a-v4a)" --state "$base"
case_run m07-recreate 0 compose container-a-v4a up -d --force-recreate api
case_run m07-ready-again 0 wait_ready http://127.0.0.1:21007
check m07-correct 0 m07-persistence container-a-v4a --base-url http://127.0.0.1:21007 --container "$api" --expected-image "$(image_id container-a-v4a)" --state "$base"
check m07-wrong-image-fails 1 m07-persistence container-a-v4a --base-url http://127.0.0.1:21007 --container "$api" --expected-image sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff --state "$base"
check m07-fixed 0 m07-persistence container-a-v4a --base-url http://127.0.0.1:21007 --container "$api" --expected-image "$(image_id container-a-v4a)" --state "$base"
case_run m07-stop 0 labctl stop --instance container-a-v4a --root "$root"
case_run m07-reset 0 labctl reset --instance container-a-v4a --root "$root" --confirm-instance container-a-v4a
case_run m07-recover 0 labctl start --instance container-a-v4a --root "$root"
case_run m07-recovered-ready 0 wait_ready http://127.0.0.1:21007
check m07-data-after-reset 0 m07-persistence container-a-v4a --base-url http://127.0.0.1:21007 --container "$api" --expected-image "$(image_id container-a-v4a)" --state "$base"
case_run m07-final-stop 0 labctl stop --instance container-a-v4a --root "$root"
fi

if [[ ! -f "$root/postgres-a-v4a/.peaky-instance.json" ]];then
case_run m08-prepare 0 labctl prepare m08 --seed 418 --instance postgres-a-v4a --root "$root" --port 21008
fi
case_run m08-start 0 labctl start --instance postgres-a-v4a --root "$root"
case_run m08-migrate 0 compose postgres-a-v4a exec -T api ticketlab migrate --apply
case_run m08-ready 0 wait_ready http://127.0.0.1:21008
base="$root/postgres-a-v4a/state/before.json"
pg=peaky301758-postgres-a-v4a-db-1
case_run m08-baseline 0 labcheck baseline m08-compose --seed 418 --instance postgres-a-v4a --base-url http://127.0.0.1:21008 --postgres-container "$pg" --expected-source "$(jq -r '.source_revision' "$root/postgres-a-v4a/state/release.json")" --state "$base"
check m08-correct 0 m08-compose postgres-a-v4a --base-url http://127.0.0.1:21008 --postgres-container "$pg" --state "$base"
case_run m08-db-stop 0 compose postgres-a-v4a stop db
sleep 2
check m08-readiness-fails 1 m08-compose postgres-a-v4a --base-url http://127.0.0.1:21008 --postgres-container "$pg" --state "$base"
case_run m08-db-fixed 0 compose postgres-a-v4a start db
case_run m08-recovered-ready 0 wait_ready http://127.0.0.1:21008
check m08-fixed 0 m08-compose postgres-a-v4a --base-url http://127.0.0.1:21008 --postgres-container "$pg" --state "$base"
case_run m08-recreate 0 compose postgres-a-v4a up -d --force-recreate
case_run m08-recreated-ready 0 wait_ready http://127.0.0.1:21008
check m08-data-after-recreate 0 m08-compose postgres-a-v4a --base-url http://127.0.0.1:21008 --postgres-container "$pg" --state "$base"
case_run m08-stop 0 labctl stop --instance postgres-a-v4a --root "$root"

case_run m09-prepare 0 labctl prepare m09 --seed 418 --instance ci-a-v4a --root "$root" --port 21009
case_run m09-start 0 labctl start --instance ci-a-v4a --root "$root"
case_run m09-ready 0 wait_ready http://127.0.0.1:21009
check m09-skipped-check-fails 1 m09-ci ci-a-v4a --base-url http://127.0.0.1:21009
source_revision=$(jq -r '.source_revision' "$root/ci-a-v4a/state/release.json")
printf 'mandatory-check=on\nartifact-source=%s\n' "$source_revision" > "$root/ci-a-v4a/config/ci.env"
check m09-correct 0 m09-ci ci-a-v4a --base-url http://127.0.0.1:21009
printf 'mandatory-check=on\nartifact-source=wrong\n' > "$root/ci-a-v4a/config/ci.env"
check m09-wrong-artifact-fails 1 m09-ci ci-a-v4a --base-url http://127.0.0.1:21009
printf 'mandatory-check=on\nartifact-source=%s\n' "$source_revision" > "$root/ci-a-v4a/config/ci.env"
check m09-fixed 0 m09-ci ci-a-v4a --base-url http://127.0.0.1:21009
case_run m09-stop 0 labctl stop --instance ci-a-v4a --root "$root"

case_run m10-prepare 0 labctl prepare m10 --seed 418 --instance restore-a-v4a --root "$root" --port 21010
case_run m10-start 0 labctl start --instance restore-a-v4a --root "$root"
case_run m10-migrate 0 compose restore-a-v4a exec -T api ticketlab migrate --apply
case_run m10-ready 0 wait_ready http://127.0.0.1:21010
base="$root/restore-a-v4a/state/before.json"
pg=peaky301758-restore-a-v4a-db-1
case_run m10-baseline 0 labcheck baseline m10-restore --seed 418 --instance restore-a-v4a --base-url http://127.0.0.1:21010 --postgres-container "$pg" --state "$base"
case_run m10-backup 0 labctl backup --instance restore-a-v4a --root "$root"
case_run m10-source-after-backup 0 wait_ready http://127.0.0.1:21010
curl -fsS -H 'Content-Type: application/json' -d '{"title":"Created strictly after backup"}' http://127.0.0.1:21010/tickets > "$root/m10-late-record.json"
case_run m10-restore 0 labctl restore --instance restore-a-v4a --root "$root"
case_run m10-restore-ready 0 wait_ready http://127.0.0.1:21110
check m10-same-db-fails 1 m10-restore restore-a-v4a --base-url http://127.0.0.1:21010 --postgres-container "$pg" --database ticketlab --restore-database ticketlab --state "$base"
check m10-correct 0 m10-restore restore-a-v4a --base-url http://127.0.0.1:21110 --postgres-container "$pg" --database ticketlab --restore-database restored_restore_a_v4a --state "$base"
check m10-repeat 0 m10-restore restore-a-v4a --base-url http://127.0.0.1:21110 --postgres-container "$pg" --database ticketlab --restore-database restored_restore_a_v4a --state "$base"
check m10-release 0 m10-release restore-a-v4a --base-url http://127.0.0.1:21010 --container peaky301758-restore-a-v4a-api-1 --expected-image "$(image_id restore-a-v4a)" --state "$base"
case_run m10-stop 0 labctl stop --instance restore-a-v4a --root "$root"
case_run m10-restore-stop 0 docker stop peaky301758-restore-a-v4a-restore

case_run m12-prepare 0 labctl prepare m12 --seed 418 --instance retry-a-v4a --root "$root" --port 21012
case_run m12-start 0 labctl start --instance retry-a-v4a --root "$root"
case_run m12-ready 0 wait_ready http://127.0.0.1:21012
base="$root/retry-a-v4a/state/before.json"
api=peaky301758-retry-a-v4a-api-1
case_run m12-baseline 0 labcheck baseline m12-retry --seed 418 --instance retry-a-v4a --base-url http://127.0.0.1:21012 --container "$api" --state "$base"
check m12-no-recreate-fails 1 m12-retry retry-a-v4a --base-url http://127.0.0.1:21012 --container "$api" --state "$base"
case_run m12-recreate 0 compose retry-a-v4a up -d --force-recreate api
case_run m12-recreated-ready 0 wait_ready http://127.0.0.1:21012
check m12-correct 0 m12-retry retry-a-v4a --base-url http://127.0.0.1:21012 --container "$api" --state "$base"
case_run m12-change-stop 0 labctl stop --instance retry-a-v4a --root "$root"
case_run m12-bad-retry-release 0 labctl select-release --release bad-retry --instance retry-a-v4a --root "$root"
case_run m12-bad-retry-start 0 labctl start --instance retry-a-v4a --root "$root"
case_run m12-bad-retry-ready 0 wait_ready http://127.0.0.1:21012
check m12-retry-and-conflict-fail 1 m12-retry retry-a-v4a --base-url http://127.0.0.1:21012 --container "$api" --state "$base"
case_run m12-fix-stop 0 labctl stop --instance retry-a-v4a --root "$root"
case_run m12-fix-release 0 labctl select-release --release good --instance retry-a-v4a --root "$root"
case_run m12-fix-start 0 labctl start --instance retry-a-v4a --root "$root"
case_run m12-fixed-ready 0 wait_ready http://127.0.0.1:21012
check m12-fixed 0 m12-retry retry-a-v4a --base-url http://127.0.0.1:21012 --container "$api" --state "$base"
case_run m12-stop 0 labctl stop --instance retry-a-v4a --root "$root"
case_run m12-reset 0 labctl reset --instance retry-a-v4a --root "$root" --confirm-instance retry-a-v4a
printf '\nROUTE_COMPLETE %s\n' "$root"
cat "$result"

#!/bin/bash
set -euo pipefail
export PATH="$full_kit:$PATH"
run_id="$full_run/project"
root="$HOME/peaky-labs/$run_id"
mkdir -m700 -p "$root"
result="$root/results.tsv"
printf 'case\texpected_exit\tactual_exit\n' > "$result"
case_run() {
 local name=$1 expected=$2;shift 2
 printf '\nCOMMAND %s: ' "$name";printf '%q ' "$@";printf '\n'
 set +e
 "$@" < /dev/null > "$root/$name.console.txt" 2>&1
 local actual=$?
 set -e
 cat "$root/$name.console.txt"
 printf '%s\t%s\t%s\n' "$name" "$expected" "$actual" >> "$result"
 if [[ $actual != "$expected" ]];then printf 'HARNESS FAILURE %s expected=%s actual=%s\n' "$name" "$expected" "$actual" >&2;exit 90;fi
}
compose() { docker compose --project-directory "$root/$1" -p "peaky301758-$1" "${@:2}"; }
wait_ready() {
 for n in {1..90};do if curl -sf --max-time 3 "$1/ready" >/dev/null;then return 0;fi;sleep 1;done
 return 2
}
check() {
 local name=$1 expected=$2;shift 2
 case_run "$name" "$expected" labcheck check m11-project --seed 418 \
  --instance project-a-v4a --workspace "$root/project-a-v4a" --base-url http://127.0.0.1:21011 \
  --postgres-container peaky301758-project-a-v4a-db-1 --database ticketlab \
  --peer-instance project-b-v4a --peer-postgres-container peaky301758-project-b-v4a-db-1 \
  --peer-database restored_project_b_v4a --peer-base-url http://127.0.0.1:21211 \
  --expected-source "$(jq -r '.source_revision' "$root/project-a-v4a/state/release.json")" \
  --state "$root/project-a-v4a/state/before-handover.json" --report "$root/$name.json" "$@"
}
if [[ ! -f "$root/project-a-v4a/.peaky-instance.json" ]];then
case_run m11-prepare 0 labctl prepare m11 --seed 418 --instance project-a-v4a --root "$root" --port 21011
fi
case_run m11-start 0 labctl start --instance project-a-v4a --root "$root"
case_run m11-schema 0 compose project-a-v4a exec -T api ticketlab migrate --apply
case_run m11-ready 0 wait_ready http://127.0.0.1:21011
source_revision=$(jq -r '.source_revision' "$root/project-a-v4a/state/release.json")
printf 'mandatory-check=on\nartifact-source=%s\n' "$source_revision" > "$root/project-a-v4a/config/ci.env"
if [[ ! -f "$root/project-a-v4a/state/before-handover.json" ]];then
case_run m11-baseline 0 labcheck baseline m11-project --seed 418 --instance project-a-v4a --base-url http://127.0.0.1:21011 --postgres-container peaky301758-project-a-v4a-db-1 --state "$root/project-a-v4a/state/before-handover.json"
fi
if [[ ! -f "$root/project-a-v4a/state/backup.json" ]];then
case_run m11-backup 0 labctl backup --instance project-a-v4a --root "$root"
fi
case_run m11-source-ready 0 wait_ready http://127.0.0.1:21011
if [[ ! -f "$root/project-b-v4a/.peaky-instance.json" ]];then
case_run m11-peer-prepare 0 labctl prepare m11 --seed 418 --instance project-b-v4a --root "$root" --port 21111
fi
case_run m11-peer-start 0 labctl start --instance project-b-v4a --root "$root"
case_run m11-import 0 labctl import-backup --instance project-b-v4a --root "$root" --backup-from "$root/project-a-v4a/state/backup.dump"
case_run m11-peer-restore 0 labctl restore --instance project-b-v4a --root "$root"
case_run m11-peer-ready 0 wait_ready http://127.0.0.1:21211
check m11-correct 0
check m11-same-instance-fails 1 --peer-instance project-a-v4a --peer-postgres-container peaky301758-project-a-v4a-db-1 --peer-database ticketlab --peer-base-url http://127.0.0.1:21011
check m11-fixed 0
case_run m11-peer-restart 0 docker restart peaky301758-project-b-v4a-restore
case_run m11-peer-after-restart-ready 0 wait_ready http://127.0.0.1:21211
check m11-after-peer-restart 0
case_run m11-peer-stop 0 labctl stop --instance project-b-v4a --root "$root"
case_run m11-peer-restore-stop 0 docker stop peaky301758-project-b-v4a-restore
case_run m11-peer-retained 0 labctl status --instance project-b-v4a --root "$root"
case_run m11-source-survived 0 wait_ready http://127.0.0.1:21011
case_run m11-source-stop 0 labctl stop --instance project-a-v4a --root "$root"
printf '\nROUTE_COMPLETE %s\n' "$root"
cat "$result"

printf "FULL_ROUTE_COMPLETE %s\n" "$HOME/peaky-labs/$full_run"

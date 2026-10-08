#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
verification_root=$(mktemp -d)
export V5_PRACTICE_ROOT="$verification_root/course-practice"
runner="$repo/practice/v5/practice.sh"
cases=0
expect() {
  local wanted=$1 label=$2 actual
  shift 2
  set +e
  "$@" > "$verification_root/last-check.txt" 2>&1
  actual=$?
  set -e
  cat "$verification_root/last-check.txt"
  test "$actual" = "$wanted" || { printf 'HARNESS FAILURE %s expected=%s actual=%s\n' "$label" "$wanted" "$actual"; exit 90; }
  cases=$((cases+1));printf 'VERIFIED %s exit=%s\n' "$label" "$actual"
}
for case_name in navigation create copies reading search streams git; do
  prepared=$(bash "$runner" prepare "$case_name")
  printf '%s\n' "$prepared"
  w=$(printf '%s\n' "$prepared" | sed -n 's/^WORKSPACE=//p')
  expect 1 "$case_name-initial-fails" bash "$runner" check "$case_name" "$w"
  cd "$w"
  case "$case_name" in
    navigation)
      (cd work/logs;cd ../reports;pwd > ../../evidence/cwd.txt)
      ls -A > evidence/hidden.txt ;;
    create)
      mkdir -p reports/2026/oct
      mkdir drafts 'release notes'
      touch note.txt
      rmdir old
      rm -- -draft ;;
    copies)
      cp app.conf archive/app.conf.before
      printf 'port=8090\n' > app.conf
      cp archive/app.conf.before app.conf
      mv report.txt archive/ ;;
    reading)
      head -n 4 app.log > evidence/first.txt
      tail -n 2 app.log > evidence/last.txt ;;
    search)
      grep -F 'ticket=T42 ' service.log > evidence/T42.log
      find tree -name '*.log' -type f > evidence/log-files.txt ;;
    streams)
      sort events.txt | uniq > evidence/unique.txt
      sort events.txt | uniq -c > evidence/counts.txt
      set +e
      bin/probe > evidence/out.txt 2> evidence/err.txt
      probe_rc=$?
      set -e
      test "$probe_rc" = 7 ;;
    git)
      printf 'port=8090\n' > config.env
      git add -- config.env
      git commit -qm 'Only required configuration' ;;
  esac
  expect 0 "$case_name-correct" bash "$runner" check "$case_name" "$w"
  case "$case_name" in
    navigation) printf '%s\n' "$w/work/logs" > evidence/cwd.txt ;;
    create) printf 'changed\n' > keep.txt ;;
    copies) printf 'port=8090\n' > archive/app.conf.before ;;
    reading) printf 'corrupted\n' > evidence/last.txt ;;
    search) grep -F ticket=T42 service.log > evidence/T42.log ;;
    streams) uniq events.txt > evidence/unique.txt ;;
    git) git add .env ;;
  esac
  expect 1 "$case_name-real-error-fails" bash "$runner" check "$case_name" "$w"
  case "$case_name" in
    navigation) (cd work/reports;pwd > ../../evidence/cwd.txt) ;;
    create) printf 'keep\n' > keep.txt ;;
    copies) printf 'port=8080\n' > archive/app.conf.before ;;
    reading) tail -2 app.log > evidence/last.txt ;;
    search) grep -F 'ticket=T42 ' service.log > evidence/T42.log ;;
    streams) sort -u events.txt > evidence/unique.txt ;;
    git) git restore --staged .env ;;
  esac
  expect 0 "$case_name-fixed-alternative" bash "$runner" check "$case_name" "$w"
done
printf 'PASS: %s real filesystem/Git checks, including initial failure, correct state, semantic failure and corrected state. Evidence: %s\n' "$cases" "$verification_root"

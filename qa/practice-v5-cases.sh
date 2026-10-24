#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/bin:/bin
cd "$1"
tool="$PWD/practice/v5/practice.sh"
sha256sum -c SHA256SUMS
bash "$tool" > help.txt
grep -q '^Usage:' help.txt
printf 'label\texpected\tactual\n' > results.tsv
run_check() {
 local label=$1 want=$2 case_name=$3 work=$4 actual
 set +e
 bash "$tool" check "$case_name" "$work" > "$PWD/$label.log" 2>&1
 actual=$?
 set -e
 printf '%s\t%s\t%s\n' "$label" "$want" "$actual" >> results.tsv
 [[ $actual == "$want" ]] || { cat "$PWD/$label.log"; exit 90; }
}
prepare() {
 local name=$1
 bash "$tool" prepare "$name" > "$name-prepare.txt"
 w=$(sed -n 's/^WORKSPACE=//p' "$name-prepare.txt")
 [[ "$w" == "$V5_PRACTICE_ROOT"/v5-"$name"-* ]] || exit 91
}
for case_name in navigation create copies reading search streams git; do
 prepare "$case_name"
 run_check "$case_name-initial" 1 "$case_name" "$w"
 case "$case_name" in
 navigation)
  (cd "$w/work/reports"; pwd > "$w/evidence/cwd.txt")
  (cd "$w"; ls -a > evidence/hidden.txt)
  ;;
 create)
  mkdir -p "$w/reports/2026/oct" "$w/drafts" "$w/release notes"
  touch "$w/note.txt"
  rmdir "$w/old"
  rm -- "$w/-draft"
  ;;
 copies)
  cp "$w/app.conf" "$w/archive/app.conf.before"
  printf 'port=8090\n' > "$w/app.conf"
  cp "$w/archive/app.conf.before" "$w/app.conf"
  mv "$w/report.txt" "$w/archive/report.txt"
  ;;
 reading)
  head -n 4 "$w/app.log" > "$w/evidence/first.txt"
  tail -n 2 "$w/app.log" > "$w/evidence/last.txt"
  ;;
 search)
  grep -F 'ticket=T42 ' "$w/service.log" > "$w/evidence/T42.log"
  (cd "$w"; find tree -type f -name '*.log' > evidence/log-files.txt)
  ;;
 streams)
  sort -u "$w/events.txt" > "$w/evidence/unique.txt"
  sort "$w/events.txt" | uniq -c > "$w/evidence/counts.txt"
  set +e
  "$w/bin/probe" > "$w/evidence/out.txt" 2> "$w/evidence/err.txt"
  probe_status=$?
  set -e
  [[ $probe_status == 7 ]] || exit 92
  ;;
 git)
  printf 'port=8090\n' > "$w/config.env"
  git -C "$w" add config.env
  git -C "$w" commit -qm 'Verified selected config change'
  ;;
 esac
 run_check "$case_name-positive" 0 "$case_name" "$w"
 positive_workspace=$w
 case "$case_name" in
 navigation) printf 'wrong\n' > "$w/evidence/cwd.txt" ;;
 create) printf 'modified\n' > "$w/keep.txt" ;;
 copies) cp "$w/archive/report.txt" "$w/report.txt" ;;
 reading) head -n 3 "$w/app.log" > "$w/evidence/first.txt" ;;
 search) grep -F 'ticket=T42' "$w/service.log" > "$w/evidence/T42.log" ;;
 streams) uniq "$w/events.txt" > "$w/evidence/unique.txt" ;;
 git)
  prepare git
  printf 'port=8090\n' > "$w/config.env"
  git -C "$w" add config.env draft.txt
  git -C "$w" commit -qm 'Intentional invalid draft inclusion'
  ;;
 esac
 run_check "$case_name-negative" 1 "$case_name" "$w"
 case "$case_name" in
 navigation) (cd "$w/work/reports"; pwd > "$w/evidence/cwd.txt") ;;
 create) printf 'keep\n' > "$w/keep.txt" ;;
 copies) rm -- "$w/report.txt" ;;
 reading) head -n 4 "$w/app.log" > "$w/evidence/first.txt" ;;
 search) grep -F 'ticket=T42 ' "$w/service.log" > "$w/evidence/T42.log" ;;
 streams) sort -u "$w/events.txt" > "$w/evidence/unique.txt" ;;
 git) w=$positive_workspace ;;
 esac
 run_check "$case_name-recovered" 0 "$case_name" "$w"
done
run_check boundary-outside-root 1 navigation "$HOME"
run_check boundary-wrong-case 1 navigation "$w"
printf 'PRACTICE_V5_VERIFIED\n'

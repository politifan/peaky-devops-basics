#!/usr/bin/env bash
# Author verification of real command behavior; no changes outside a fresh temp directory.
set -euo pipefail
export LC_ALL=C
base=$(mktemp -d)
cd "$base"
passed=0
assert_eq() {
  if [[ "$1" != "$2" ]]; then printf 'FAIL: %s (actual=%q expected=%q)\n' "$3" "$1" "$2" >&2; exit 1; fi
  passed=$((passed+1)); printf 'PASS %02d %s\n' "$passed" "$3"
}
mkdir -p work/logs 'release notes'
cd work/logs
assert_eq "$(pwd)" "$base/work/logs" 'absolute working directory'
cd ..
assert_eq "$(pwd)" "$base/work" 'parent directory'
cd "$base"
before=$PWD
if cd absent 2>/dev/null; then exit 1; fi
assert_eq "$PWD" "$before" 'failed cd preserves directory'
cd 'release notes'
assert_eq "$PWD" "$base/release notes" 'quoted space is one argument'
cd "$base"
touch .hidden visible
assert_eq "$(ls -a | grep -Fx .hidden)" '.hidden' 'ls -a includes hidden'
assert_eq "$(ls | grep -Fc .hidden || true)" 0 'plain ls omits hidden'
mkdir -p nested/path
mkdir -p nested/path
test -d nested/path
printf x > plain
if mkdir -p plain/child 2>/dev/null; then exit 1; fi
passed=$((passed+1)); printf 'PASS %02d file cannot become parent directory\n' "$passed"
mkdir scratch
printf keep > scratch/file
if rmdir scratch 2>/dev/null; then exit 1; fi
assert_eq "$(cat scratch/file)" keep 'rmdir preserves nonempty directory'
rm scratch/file
rmdir scratch
test ! -e scratch
printf hello > touched
touch touched
assert_eq "$(cat touched)" hello 'touch preserves existing content'
touch -- -draft
rm -- -draft
test ! -e ./-draft
rm -f absent-file
passed=$((passed+1)); printf 'PASS %02d rm handles exact option-like and absent names\n' "$passed"
printf 'port=8080\n' > app.conf
cp app.conf copy.conf
printf 'port=9090\n' > app.conf
assert_eq "$(cat copy.conf)" port=8080 'copy is independent'
mv copy.conf old.conf
test ! -e copy.conf
assert_eq "$(cat old.conf)" port=8080 'mv preserves content at new path'
cp -R work work-copy
test -d work-copy/logs
assert_eq "$(stat -c %s old.conf)" 10 'stat counts bytes'
file old.conf | grep -q text
printf 'A\nB\nC\nD\n' > sequence
assert_eq "$(head -n 2 sequence)" $'A\nB' 'head selects beginning'
assert_eq "$(tail -n 2 sequence)" $'C\nD' 'tail selects end'
assert_eq "$(wc -l < sequence | tr -d ' ')" 4 'reading preserves source'
printf 'INFO ticket=42 saved\nERROR ticket=420 failed\nERROR ticket=42 retry\n' > events
assert_eq "$(grep -Fc '42' events)" 3 'broad grep includes similar ID'
assert_eq "$(grep -Fc 'ticket=42 ' events)" 2 'field boundary excludes similar ID'
assert_eq "$(grep -Fn 'ticket=42 ' events)" $'1:INFO ticket=42 saved\n3:ERROR ticket=42 retry' 'grep -n actual line numbers'
assert_eq "$(grep -vc ERROR events)" 1 'grep -v inverse selection'
set +e
grep -F absent events >/dev/null
grep_status=$?
set -e
assert_eq "$grep_status" 1 'grep no match exit code'
mkdir -p logs/archive logs/fake.log
touch logs/app.log logs/archive/old.log logs/readme.txt
assert_eq "$(find logs -type f -name '*.log' | sort)" $'logs/app.log\nlogs/archive/old.log' 'find selects files not same-suffix directory'
printf 'old\n' > redirect
printf 'new\n' > redirect
assert_eq "$(cat redirect)" new 'redirect replaces content'
printf 'next\n' >> redirect
assert_eq "$(cat redirect)" $'new\nnext' 'append preserves prior content'
printf 'A\nB\nA\n' > names
assert_eq "$(uniq names)" $'A\nB\nA' 'uniq only removes adjacent repeats'
assert_eq "$(sort names | uniq)" $'A\nB' 'sort then uniq groups values'
assert_eq "$(printf '10\n2\n' | sort -n)" $'2\n10' 'numeric sort'
assert_eq "$(printf 'A\nB' | wc -l | tr -d ' ')" 1 'wc -l counts newline characters'
set +e
set +o pipefail
false | tee empty >/dev/null
without=$?
set -o pipefail
false | tee empty >/dev/null
with=$?
set -e
assert_eq "$without" 0 'pipeline default status follows tee'
assert_eq "$with" 1 'pipefail preserves upstream failure'
printf 'VERIFIED %s assertions in isolated directory %s\n' "$passed" "$base"

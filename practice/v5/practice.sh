#!/usr/bin/env bash
set -euo pipefail
action=${1:-help}
case_name=${2:-}
workspace=${3:-}
home_root="${V5_PRACTICE_ROOT:-$HOME/course-practice}"
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
file_equals() { test -f "$1" && diff -u <(printf '%s' "$2") "$1" >/dev/null || fail "$3"; }
case "$action" in
  prepare)
    case "$case_name" in navigation|create|copies|reading|search|streams|git) ;; *) fail 'Choose navigation/create/copies/reading/search/streams/git' ;; esac
    mkdir -p "$home_root"
    workspace=$(mktemp -d "$home_root/v5-$case_name-XXXXXX")
    printf '%s\n' "$case_name" > "$workspace/.v5-case"
    mkdir -p "$workspace/evidence" "$workspace/bin"
    case "$case_name" in
      navigation)
        mkdir -p "$workspace/work/logs" "$workspace/work/reports" "$workspace/release notes"
        printf 'MODE=training\n' > "$workspace/.env" ;;
      create)
        mkdir "$workspace/old"
        printf 'keep original\n' > "$workspace/note.txt"
        printf 'keep\n' > "$workspace/keep.txt"
        printf 'temporary\n' > "$workspace/-draft" ;;
      copies)
        mkdir "$workspace/archive"
        printf 'port=8080\n' > "$workspace/app.conf"
        printf 'shift=night\n' > "$workspace/report.txt" ;;
      reading)
        printf 'start\nrequest-1\nrequest-2\nerror\nrecovered\nstop\n' > "$workspace/app.log" ;;
      search)
        mkdir -p "$workspace/tree/sub/dir.log"
        printf 'log\n' > "$workspace/tree/a.log"
        printf 'log\n' > "$workspace/tree/sub/b.log"
        printf 'note\n' > "$workspace/tree/sub/note.txt"
        printf 'INFO ticket=T42 created\nINFO ticket=T420 neighbour\nERROR ticket=T42 write_failed\nINFO ticket=T43 other\nINFO ticket=T42 recovered\n' > "$workspace/service.log" ;;
      streams)
        printf 'beta\nalpha\nbeta\ngamma\nalpha\n' > "$workspace/events.txt"
        printf '#!/usr/bin/env bash\nprintf "normal\\n"\nprintf "diagnostic\\n" >&2\nexit 7\n' > "$workspace/bin/probe"
        chmod u+x "$workspace/bin/probe" ;;
      git)
        printf 'port=8080\n' > "$workspace/config.env"
        printf 'My training project\n' > "$workspace/README.md"
        printf 'draft\n' > "$workspace/draft.txt"
        printf 'TRAINING_DUMMY_TOKEN=not-a-secret\n' > "$workspace/.env"
        git -C "$workspace" init -q
        git -C "$workspace" config user.name 'Course learner'
        git -C "$workspace" config user.email 'learner@example.invalid'
        git -C "$workspace" add README.md config.env
        git -C "$workspace" commit -qm 'Initial training files' ;;
    esac
    printf 'WORKSPACE=%s\n' "$workspace"
    printf 'Open this directory in your Bash terminal. Keep each attempt separate.\n'
    printf 'cd -- %q\n' "$workspace"
    printf 'After completing the task, use this check command:\n'
    printf 'bash %q check %q %q\n' "$(realpath "${BASH_SOURCE[0]}")" "$case_name" "$workspace"
    ;;
  check)
    test -n "$workspace" || fail 'Specify the exact WORKSPACE printed by prepare.'
    workspace=$(realpath -e "$workspace")
    allowed=$(realpath -e "$home_root")
    [[ "$workspace" == "$allowed"/v5-"$case_name"-* ]] || fail 'Not a prepared personal v5 workspace.'
    file_equals "$workspace/.v5-case" "$case_name"$'\n' 'Wrong case or missing preparation.'
    cd "$workspace"
    case "$case_name" in
      navigation)
        file_equals evidence/cwd.txt "$workspace/work/reports"$'\n' 'Save pwd from work/reports in evidence/cwd.txt.'
        test -f evidence/hidden.txt || fail 'Save ls output including hidden names in evidence/hidden.txt.'
        grep -q '^\.env$' evidence/hidden.txt || fail 'Hidden .env is missing from your list.'
        test -d 'release notes' || fail 'The directory name with a space was changed.' ;;
      create)
        test -d reports/2026/oct && test -d drafts && test -d 'release notes' || fail 'Required directory structure is incomplete.'
        test ! -e old && test ! -e ./-draft || fail 'Remove the empty old directory and only the -draft file.'
        file_equals note.txt $'keep original\n' 'touch must preserve note.txt contents.'
        file_equals keep.txt $'keep\n' 'The neighbouring keep.txt must remain unchanged.' ;;
      copies)
        file_equals app.conf $'port=8080\n' 'Restore the working configuration.'
        file_equals archive/app.conf.before $'port=8080\n' 'Keep the original backup.'
        file_equals archive/report.txt $'shift=night\n' 'Move the report with unchanged contents.'
        test ! -e report.txt || fail 'The original path must disappear after mv.' ;;
      reading)
        file_equals app.log $'start\nrequest-1\nrequest-2\nerror\nrecovered\nstop\n' 'Reading must not change the original log.'
        file_equals evidence/first.txt $'start\nrequest-1\nrequest-2\nerror\n' 'Save the first four lines.'
        file_equals evidence/last.txt $'recovered\nstop\n' 'Save the last two lines.' ;;
      search)
        file_equals evidence/T42.log $'INFO ticket=T42 created\nERROR ticket=T42 write_failed\nINFO ticket=T42 recovered\n' 'Select all T42 events, excluding T420 and T43.'
        test -f evidence/log-files.txt || fail 'Save the find result.'
        diff -u <(printf 'tree/a.log\ntree/sub/b.log\n') <(sort evidence/log-files.txt) >/dev/null || fail 'Find ordinary *.log files, not the directory dir.log.' ;;
      streams)
        file_equals evidence/unique.txt $'alpha\nbeta\ngamma\n' 'Remove non-adjacent duplicate events.'
        file_equals evidence/out.txt $'normal\n' 'Save only stdout in out.txt.'
        file_equals evidence/err.txt $'diagnostic\n' 'Save only stderr in err.txt.'
        test -f evidence/counts.txt || fail 'Save sort | uniq -c output.'
        diff -u <(printf '2 alpha\n2 beta\n1 gamma\n') <(awk '{$1=$1;print}' evidence/counts.txt) >/dev/null || fail 'Counts must include all non-adjacent duplicates.' ;;
      git)
        content=$(git show HEAD:config.env)
        test "$content" = 'port=8090' || fail 'Commit the required configuration change.'
        git cat-file -e HEAD:draft.txt 2>/dev/null && fail 'draft.txt must not be in HEAD.'
        git cat-file -e HEAD:.env 2>/dev/null && fail '.env must not be in HEAD.'
        test -z "$(git log --all --format=%H -- .env draft.txt)" || fail 'Do not include .env/draft.txt in earlier commits either; start a fresh training attempt if needed.'
        test -f draft.txt && test -f .env || fail 'Keep local files outside the commit.'
        test -z "$(git diff --cached)" || fail 'Finish the selected commit.' ;;
      *) fail 'Unknown case.' ;;
    esac
    printf 'PASS: %s — observed files/state match the task. This is local self-check, not Stepik server grading.\n' "$case_name"
    ;;
  *) printf 'Usage: bash practice/v5/practice.sh prepare CASE\n       bash practice/v5/practice.sh check CASE WORKSPACE\n' ;;
esac

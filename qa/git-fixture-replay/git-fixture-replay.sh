#!/usr/bin/env bash
set -euo pipefail
: "${RUNNER_TEMP:?isolated CI runner only}"
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo 'CI only' >&2; exit 2; }
[[ "${RUNNER_OS:-}" == Linux && "${GITHUB_REF:-}" == refs/heads/codex/devops-quality-20261023 ]] || exit 2
[[ -d "$RUNNER_TEMP" && ! -L "$RUNNER_TEMP" ]] || exit 2
archive=$1
[[ -f "$archive" && ! -L "$archive" ]] || exit 2
# Inherited Git routing/config must not affect the fixture.
for key in $(env | cut -d= -f1 | grep '^GIT_' || true); do unset "$key"; done
printf '%s  %s\n' dc6d40153e4d7100a922d01897e30707920cb3ba4a6148a248c0005a7e6f8dcb "$archive" | sha256sum -c -
root=$(mktemp -d "$RUNNER_TEMP/basic-git-replay.XXXXXX")
mkdir "$root/attempts" "$root/hooks"
printf '' > "$root/gitconfig"
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$root/gitconfig" GIT_CONFIG_SYSTEM="$root/gitconfig" GIT_TERMINAL_PROMPT=0
export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0="$root/hooks" GIT_CONFIG_KEY_1=commit.gpgsign GIT_CONFIG_VALUE_1=false
export V5_PRACTICE_ROOT="$root/attempts"
python3 - "$archive" "$root/practice.sh" <<'PYSAFE'
import sys,tarfile
with tarfile.open(sys.argv[1]) as t:
    m=t.getmember('practice/v5/practice.sh')
    assert m.isfile() and not m.issym() and not m.islnk() and m.size<100000
    data=t.extractfile(m).read()
with open(sys.argv[2], 'xb') as f:f.write(data)
PYSAFE
prepared=$(bash "$root/practice.sh" prepare git)
printf '%s\n' "$prepared"
workspace=$(printf '%s\n' "$prepared" | sed -n 's/^WORKSPACE=//p')
[[ "$workspace" == "$root/attempts/"* ]] || exit 2
cd -- "$workspace"
[[ $(cat config.env) == port=8080 ]]
run() { printf '\n$ %s\n' "$1"; bash -c "$1"; printf '[exit=0]\n'; }
run 'git status --short'
run "printf 'port=8090\\n' > config.env"
run 'git diff -- config.env'
run 'git add config.env'
run 'git diff --cached -- config.env'
run 'git restore --staged config.env'
run 'git status --short'
run 'git add config.env'
run "git commit -m 'Change training port'"
run 'git show HEAD:config.env'
run 'git status --short'
bash "$root/practice.sh" check git "$workspace"

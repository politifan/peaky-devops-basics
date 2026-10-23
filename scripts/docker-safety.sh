#!/usr/bin/env bash
# Source this before registering cleanup traps. Never silently select a new daemon.
require_local_docker() {
  local key
  for key in DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH COMPOSE_FILE PEAKY_AUTHOR_PROBE_SSH; do
    if [[ -n "${!key:-}" ]]; then
      printf 'Docker operation refused: unset %s and use a dedicated local training daemon\n' "$key" >&2
      return 64
    fi
  done
  [[ "$(command /usr/bin/docker context show)" == default ]] || return 64
  [[ "$(command /usr/bin/docker context inspect default --format '{{.Endpoints.docker.Host}}')" == unix:///var/run/docker.sock ]] || return 64
}

docker() {
  require_local_docker || return $?
  # Pin every operation, including cleanup, independently of later context changes.
  command /usr/bin/docker --host unix:///var/run/docker.sock "$@"
}

require_local_docker || return $?

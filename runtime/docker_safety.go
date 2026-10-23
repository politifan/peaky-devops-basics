package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const localDockerEndpoint = "unix:///var/run/docker.sock"

// Mutation entry points call guardLocalDocker first. Pin the actual command too,
// so changing the active context between that check and execution cannot retarget it.
func pinnedDockerCommand(ctx context.Context, args ...string) *exec.Cmd {
	all := append([]string{"--host", localDockerEndpoint}, args...)
	return exec.CommandContext(ctx, "/usr/bin/docker", all...)
}

// Only context metadata is queried here; no daemon or workload is contacted.
var dockerSafetyProbe = func(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/docker", args...).Output()
}

func guardLocalDocker() error {
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "COMPOSE_FILE", "PEAKY_AUTHOR_PROBE_SSH"} {
		if os.Getenv(key) != "" {
			return fmt.Errorf("Docker mutation refused: unset %s; use the dedicated local training daemon", key)
		}
	}
	active, err := dockerSafetyProbe("context", "show")
	if err != nil || strings.TrimSpace(string(active)) != "default" {
		return fmt.Errorf("Docker mutation requires the default local context")
	}
	host, err := dockerSafetyProbe("context", "inspect", "default", "--format", "{{.Endpoints.docker.Host}}")
	if err != nil || strings.TrimSpace(string(host)) != localDockerEndpoint {
		return fmt.Errorf("Docker mutation requires %s", localDockerEndpoint)
	}
	return nil
}

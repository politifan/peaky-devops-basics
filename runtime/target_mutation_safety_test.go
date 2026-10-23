package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUnconfirmedTargetNeverReceivesFurtherRequests(t *testing.T) {
	for _, field := range []string{"instance_id", "release_version", "source_revision", "api_contract"} {
		t.Run(field, func(t *testing.T) {
			var further atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/version" {
					version := map[string]string{"instance_id": "owned", "release_version": "known", "source_revision": "sha", "api_contract": "1.0"}
					version[field] = "untrusted"
					json.NewEncoder(w).Encode(version)
					return
				}
				further.Add(1)
				fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()
			report := Report{Checks: []Check{}}
			checker := Checker{srv.URL, "owned", "known", "sha", srv.Client(), &report}
			checker.httpChecks(Profile{Mandatory: []string{"target", "version", "health", "ready", "ticket.create", "ticket.read", "ticket.invalid"}}, "")
			if further.Load() != 0 {
				t.Fatalf("identity failure caused %d later requests", further.Load())
			}
			blocked := false
			for _, check := range report.Checks {
				if check.ID == "ticket.create" && check.Status == "not_checked" {
					blocked = true
				}
			}
			if !blocked {
				t.Fatal("dependent write was not explicitly blocked")
			}
		})
	}
}

func TestConfirmedTargetStillExercisesWrite(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"instance_id":"owned","release_version":"known","source_revision":"sha","api_contract":"1.0"}`)
			return
		}
		if r.Method == "POST" {
			writes.Add(1)
		}
		w.WriteHeader(503)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	report := Report{Checks: []Check{}}
	checker := Checker{srv.URL, "owned", "known", "sha", srv.Client(), &report}
	checker.httpChecks(Profile{Mandatory: []string{"target", "version", "ticket.create"}}, "")
	if writes.Load() != 1 {
		t.Fatalf("expected one real check write, got %d", writes.Load())
	}
}

func TestMutationCommandPinsEndpointWithoutRunningIt(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "changed-after-check")
	command := pinnedDockerCommand(context.Background(), "compose", "down", "-v")
	if command.Path != "/usr/bin/docker" || strings.Join(command.Args, " ") !=
		"/usr/bin/docker --host unix:///var/run/docker.sock compose down -v" {
		t.Fatalf("mutation command lost its fixed endpoint: %v", command.Args)
	}
	// Constructing exec.Cmd is harmless; never call Run/Start in this test.
}

var dockerSafetyKeys = []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "COMPOSE_FILE", "PEAKY_AUTHOR_PROBE_SSH"}

func clearDockerSafetyEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range dockerSafetyKeys {
		t.Setenv(key, "")
	}
}

func TestDockerOverridesRejectedWithoutProbeOrMutation(t *testing.T) {
	clearDockerSafetyEnvironment(t)
	original := dockerSafetyProbe
	defer func() { dockerSafetyProbe = original }()
	dockerSafetyProbe = func(...string) ([]byte, error) { t.Fatal("probe reached after unsafe override"); return nil, nil }
	for _, key := range dockerSafetyKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "external")
			if guardLocalDocker() == nil {
				t.Fatal("unsafe override accepted")
			}
		})
	}
}

func TestDockerContextAndEndpointFailClosed(t *testing.T) {
	clearDockerSafetyEnvironment(t)
	original := dockerSafetyProbe
	defer func() { dockerSafetyProbe = original }()
	cases := []struct {
		context, endpoint string
		allowed           bool
	}{
		{"default", localDockerEndpoint, true},
		{"production", localDockerEndpoint, false},
		{"default", "ssh://remote", false},
		{"default", "tcp://127.0.0.1:2375", false},
		{"default", "unix:///run/user/1000/docker.sock", false},
	}
	for _, test := range cases {
		dockerSafetyProbe = func(args ...string) ([]byte, error) {
			if strings.Join(args, " ") == "context show" {
				return []byte(test.context + "\n"), nil
			}
			return []byte(test.endpoint + "\n"), nil
		}
		if allowed := guardLocalDocker() == nil; allowed != test.allowed {
			t.Fatalf("unexpected policy result for %+v", test)
		}
	}
	dockerSafetyProbe = func(...string) ([]byte, error) { return nil, fmt.Errorf("metadata unavailable") }
	if guardLocalDocker() == nil {
		t.Fatal("metadata error authorized Docker")
	}
}

func TestDockerMutationEntrypointsRejectBeforeWork(t *testing.T) {
	clearDockerSafetyEnvironment(t)
	t.Setenv("DOCKER_HOST", "ssh://production.invalid")
	original := dockerSafetyProbe
	defer func() { dockerSafetyProbe = original }()
	dockerSafetyProbe = func(...string) ([]byte, error) { t.Fatal("unexpected Docker probe"); return nil, nil }
	instance := Instance{InstanceID: "safe-instance", ModuleID: "m10"}
	if composeRun("does-not-exist", instance, "down", "-v") == nil {
		t.Fatal("compose mutation accepted")
	}
	if dockerReset("does-not-exist", instance) == nil {
		t.Fatal("reset accepted")
	}
	if dockerControl("start", "does-not-exist", instance) != 64 {
		t.Fatal("start not rejected")
	}
	if backupControl("restore", "does-not-exist", instance) != 64 {
		t.Fatal("restore not rejected")
	}
	if selectControl("does-not-exist", instance, "good") != 64 {
		t.Fatal("release mutation not rejected")
	}
}

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ProbeSSH struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	KeyFile    string `json:"key_file"`
	KnownHosts string `json:"known_hosts"`
}

func probeSSH() (ProbeSSH, bool) {
	path := os.Getenv("PEAKY_AUTHOR_PROBE_SSH")
	if path == "" {
		return ProbeSSH{}, false
	}
	b, e := os.ReadFile(path)
	var conf ProbeSSH
	if e != nil || json.Unmarshal(b, &conf) != nil || conf.Host != "127.0.0.1" || conf.Port < 1024 || conf.User != "grader" {
		return ProbeSSH{}, true
	}
	return conf, true
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func probeCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	conf, remote := probeSSH()
	if !remote {
		return exec.CommandContext(ctx, name, args...)
	}
	command := ""
	if name == "/usr/bin/git" {
		args = append([]string{"-c", "safe.directory=*", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)
	}
	command = "sudo -n "
	for _, arg := range append([]string{name}, args...) {
		command += shellQuote(arg) + " "
	}
	// Strict host verification, author-owned key and fixed loopback VM only.
	return exec.CommandContext(ctx, "ssh", "-p", strconv.Itoa(conf.Port), "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+conf.KnownHosts, "-i", conf.KeyFile, conf.User+"@"+conf.Host, command)
}
func probeReadFile(path string) ([]byte, error) {
	if _, remote := probeSSH(); !remote {
		return os.ReadFile(path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return probeCommand(ctx, "/bin/cat", "--", path).Output()
}
func probeFileMode(path string) (os.FileMode, error) {
	if _, remote := probeSSH(); !remote {
		st, e := os.Stat(path)
		if e != nil {
			return 0, e
		}
		return st.Mode().Perm(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, e := probeCommand(ctx, "/usr/bin/stat", "-c", "%a", "--", path).Output()
	if e != nil {
		return 0, e
	}
	n, e := strconv.ParseUint(strings.TrimSpace(string(out)), 8, 32)
	return os.FileMode(n), e
}

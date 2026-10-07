package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Assignment struct {
	Seed            int    `json:"variant_seed"`
	ID              string `json:"id"`
	CourseID        int    `json:"course_id"`
	Instance        string `json:"instance_id"`
	Profile         string `json:"profile_id"`
	ProfileVersion  string `json:"profile_version"`
	BaseURL         string `json:"base_url"`
	Workspace       string `json:"workspace"`
	State           string `json:"state"`
	ExpectedRelease string `json:"expected_release"`
	ExpectedSource  string `json:"expected_source"`
	VerifiedStudent string `json:"verified_student_id"`
	BindingStatus   string `json:"binding_status"`
	Container       string `json:"container"`
	ExpectedImage   string `json:"expected_image"`
	Postgres        string `json:"postgres_container"`
	Database        string `json:"database"`
	RestoreDatabase string `json:"restore_database"`
	ProbeSSHConfig  string `json:"probe_ssh_config"`
	PeerWorkspace   string `json:"peer_workspace"`
	PeerInstance    string `json:"peer_instance"`
	PeerPostgres    string `json:"peer_postgres_container"`
	PeerDatabase    string `json:"peer_database"`
	PeerURL         string `json:"peer_base_url"`
}
type Job struct {
	ID            string `json:"id"`
	Assignment    string `json:"assignment_id"`
	Created       string `json:"created_utc"`
	Finished      string `json:"finished_utc"`
	Status        string `json:"status"`
	ExitCode      int    `json:"exit_code"`
	Report        string `json:"report_path"`
	Scope         string `json:"scope"`
	PlatformGrade string `json:"platform_grade"`
}

func workerMain(args []string) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("labworker serve --listen 127.0.0.1:27990 --registry PATH --data-dir PATH --token-file PATH")
		return 0
	}
	if args[0] != "serve" {
		return 64
	}
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := f.String("listen", "127.0.0.1:27990", "closed author control")
	registry := f.String("registry", "", "author assignment registry")
	data := f.String("data-dir", "", "persistent jobs")
	tokenFile := f.String("token-file", "", "private author bearer token file")
	if f.Parse(args[1:]) != nil {
		return 64
	}
	if !filepath.IsAbs(*data) {
		return 64
	}
	token, e := os.ReadFile(*tokenFile)
	if e != nil || len(token) < 32 {
		return 2
	}
	var assigned []Assignment
	b, e := os.ReadFile(*registry)
	if e != nil || json.Unmarshal(b, &assigned) != nil {
		return 2
	}
	assignments := map[string]Assignment{}
	var profiles map[string]Profile
	if json.Unmarshal(profileBytes, &profiles) != nil {
		return 2
	}
	for _, a := range assigned {
		profile, exists := profiles[a.Profile]
		if a.CourseID != 301758 || !exists || a.ProfileVersion != profile.Version || a.ID == "" || a.Instance == "" {
			return 64
		}
		if _, duplicate := assignments[a.ID]; duplicate {
			return 64
		}
		if a.Seed == 0 {
			a.Seed = 417
		}
		if a.Seed < 1 || a.Seed > 999999 {
			return 64
		}
		assignments[a.ID] = a
	}
	os.MkdirAll(*data, 0700)
	mu := sync.Mutex{}
	busy := map[string]bool{}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		want := "Bearer " + string(token)
		got := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
			problem(w, 401, "author_auth_required")
			return false
		}
		return true
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"status": "alive", "stepik": "not_connected"})
	})
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		id := r.PathValue("id")
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
			problem(w, 400, "invalid_job")
			return
		}
		b, e := os.ReadFile(filepath.Join(*data, id, "job.json"))
		if e != nil {
			problem(w, 404, "job_not_found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var in struct {
			Assignment string `json:"assignment_id"`
			Submission string `json:"submission_id"`
		}
		if !input(w, r, &in) {
			return
		}
		a, ok := assignments[in.Assignment]
		if !ok || len(in.Submission) < 1 || len(in.Submission) > 128 {
			problem(w, 400, "unknown_assignment_or_submission")
			return
		}
		hash := sha256.Sum256([]byte(in.Assignment + "\x00" + in.Submission))
		id := hex.EncodeToString(hash[:])
		dir := filepath.Join(*data, id)
		mu.Lock()
		defer mu.Unlock()
		if old, e := os.ReadFile(filepath.Join(dir, "job.json")); e == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write(old)
			return
		}
		if busy[a.Instance] {
			problem(w, 409, "instance_busy")
			return
		}
		busy[a.Instance] = true
		job := Job{id, a.ID, now(), "", "queued", 2, filepath.Join(dir, "report.json"), "host_observed_author_test", "not_submitted_to_stepik"}
		if e := atomicJSON(filepath.Join(dir, "job.json"), job, true); e != nil {
			delete(busy, a.Instance)
			problem(w, 503, "job_storage_unavailable")
			return
		}
		go func() {
			job.Status = "running"
			atomicJSON(filepath.Join(dir, "job.json"), job, false)
			exe, _ := os.Executable()
			checker := filepath.Join(filepath.Dir(exe), "labcheck")
			argv := []string{"check", a.Profile, "--instance", a.Instance, "--seed", strconv.Itoa(a.Seed), "--report", job.Report, "--expected-release", a.ExpectedRelease, "--expected-source", a.ExpectedSource}
			if a.BaseURL != "" {
				argv = append(argv, "--base-url", a.BaseURL)
			}
			if a.Workspace != "" {
				argv = append(argv, "--workspace", a.Workspace)
			}
			if a.State != "" {
				argv = append(argv, "--state", a.State)
			}
			for flag, value := range map[string]string{"--container": a.Container, "--expected-image": a.ExpectedImage, "--postgres-container": a.Postgres, "--database": a.Database, "--restore-database": a.RestoreDatabase, "--peer-workspace": a.PeerWorkspace, "--peer-instance": a.PeerInstance, "--peer-postgres-container": a.PeerPostgres, "--peer-database": a.PeerDatabase, "--peer-base-url": a.PeerURL} {
				if value != "" {
					argv = append(argv, flag, value)
				}
			}
			log, e := os.OpenFile(filepath.Join(dir, "console.txt"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
			if e == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, checker, argv...)
				if a.ProbeSSHConfig != "" {
					cmd.Env = append(os.Environ(), "PEAKY_AUTHOR_PROBE_SSH="+a.ProbeSSHConfig)
				}
				cmd.Stdout = log
				cmd.Stderr = log
				e = cmd.Run()
				log.Close()
				job.ExitCode = 0
				if e != nil {
					job.ExitCode = 2
					if ee, ok := e.(*exec.ExitError); ok {
						job.ExitCode = ee.ExitCode()
					}
				}
			} else {
				job.ExitCode = 2
			}
			job.Status = "passed"
			if job.ExitCode == 1 {
				job.Status = "failed"
			}
			if job.ExitCode != 0 && job.ExitCode != 1 {
				job.Status = "infrastructure_failure"
			}
			job.Finished = now()
			atomicJSON(filepath.Join(dir, "job.json"), job, false)
			mu.Lock()
			delete(busy, a.Instance)
			mu.Unlock()
		}()
		reply(w, 202, job)
	})
	// Interrupted jobs never turn into successful jobs after a service restart.
	entries, _ := os.ReadDir(*data)
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		p := filepath.Join(*data, ent.Name(), "job.json")
		b, e := os.ReadFile(p)
		var j Job
		if e == nil && json.Unmarshal(b, &j) == nil && (j.Status == "running" || j.Status == "queued") {
			j.Status = "infrastructure_failure"
			j.ExitCode = 2
			j.Finished = now()
			atomicJSON(p, j, false)
		}
	}
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	if e := server.ListenAndServe(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	return 0
}

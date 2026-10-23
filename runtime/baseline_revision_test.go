package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBaselineSourceRevisionRoundTrip(t *testing.T) {
	for _, revision := range []string{strings.Repeat("a1", 20), strings.Repeat("B2", 32)} {
		t.Run(strconv.Itoa(len(revision))+"-hex", func(t *testing.T) {
			ticket := Ticket{ID: strings.Repeat("c", 32), Title: "before operation", Status: "open"}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/version":
					json.NewEncoder(w).Encode(map[string]string{"instance_id": "baseline-test", "source_revision": revision, "release_version": "3.0.0-good", "api_contract": "1.0"})
				case r.URL.Path == "/tickets" && r.Method == "POST":
					w.WriteHeader(201)
					json.NewEncoder(w).Encode(ticket)
				case r.URL.Path == "/tickets" && r.Method == "GET":
					json.NewEncoder(w).Encode(map[string]any{"items": []Ticket{ticket}, "total": 1})
				default:
					t.Errorf("unexpected baseline request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "baseline.json")
			code := checkMain([]string{"baseline", "m06-service", "--instance", "baseline-test", "--seed", "418", "--base-url", srv.URL, "--expected-source", revision, "--state", path})
			if code != 0 {
				t.Fatalf("baseline writer exit=%d", code)
			}
			saved, err := loadBaseline(path, "baseline-test")
			if err != nil {
				t.Fatalf("writer output rejected by reader: %v", err)
			}
			if saved.SourceRevision != revision || saved.Seed != 418 || len(saved.Tickets) != 1 || saved.Tickets[0] != ticket {
				t.Fatalf("baseline round trip changed metadata/data: %+v", saved)
			}
		})
	}
}

func TestBaselineRejectsInvalidSourceRevisions(t *testing.T) {
	invalid := []string{"", "unbuilt", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("g", 40), strings.Repeat("z", 64), " " + strings.Repeat("a", 39), strings.Repeat("a", 39) + "\n", "sha256:" + strings.Repeat("a", 64)}
	for _, revision := range invalid {
		if validSourceRevision(revision) {
			t.Fatalf("invalid revision accepted: %q", revision)
		}
		path := filepath.Join(t.TempDir(), "baseline.json")
		saved := Baseline{SchemaVersion: "1.0", Profile: "m06-service", Instance: "baseline-test", Created: now(), Tickets: []Ticket{{ID: strings.Repeat("c", 32), Title: "old", Status: "open"}}, ProfileVersion: "1.0", SourceRevision: revision, Seed: 418}
		if err := atomicJSON(path, saved, true); err != nil {
			t.Fatal(err)
		}
		if _, err := loadBaseline(path, "baseline-test"); err == nil {
			t.Fatalf("reader accepted invalid revision: %q", revision)
		}
	}
}

func TestBaselineInvalidRevisionRefusedBeforeHTTPOrFile(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "baseline.json")
	code := checkMain([]string{"baseline", "m06-service", "--instance", "baseline-test", "--base-url", srv.URL, "--expected-source", strings.Repeat("g", 40), "--state", path})
	if code != 64 {
		t.Fatalf("invalid metadata exit=%d, want64", code)
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid provenance caused %d HTTP requests", requests.Load())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid baseline was created or unexpected stat error: %v", err)
	}
}

func TestBaselineUnconfirmedAPIContractRefusedBeforePOST(t *testing.T) {
	for _, contract := range []string{"2.0", ""} {
		name := contract
		if name == "" {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			var requests, posts atomic.Int32
			revision := strings.Repeat("a", 40)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method == "POST" {
					posts.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				version := map[string]string{"instance_id": "baseline-test", "source_revision": revision, "release_version": "3.0.0-good"}
				if contract != "" {
					version["api_contract"] = contract
				}
				json.NewEncoder(w).Encode(version)
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "baseline.json")
			code := checkMain([]string{"baseline", "m06-service", "--instance", "baseline-test", "--base-url", srv.URL, "--expected-source", revision, "--state", path})
			if code != 1 {
				t.Fatalf("unconfirmed API contract exit=%d, want1", code)
			}
			if requests.Load() != 1 || posts.Load() != 0 {
				t.Fatalf("wanted only GET /version; requests=%d posts=%d", requests.Load(), posts.Load())
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("baseline created or unexpected stat error: %v", err)
			}
		})
	}
}

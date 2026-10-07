package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTargetBoundary(t *testing.T) {
	for _, u := range []string{"https://127.0.0.1:18003", "http://169.254.169.254", "http://178.155.74.159", "http://user:pass@127.0.0.1:18003", "http://127.0.0.1:18003/path", "http://127.0.0.1:18003?target=x", "http://localhost:18003"} {
		if safeBaseURL(u) {
			t.Fatalf("unsafe target accepted %s", u)
		}
	}
	if !safeBaseURL("http://127.0.0.1:18003") {
		t.Fatal("assigned loopback refused")
	}
}
func TestRedirectNotFollowed(t *testing.T) {
	reached := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; fmt.Fprint(w, `{"ok":true}`) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	client := redirect.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect forbidden") }
	c := Checker{base: redirect.URL, client: client}
	_, _, e := c.request("GET", "/health", nil, "")
	if e == nil || reached {
		t.Fatal("redirect escaped assigned target")
	}
}
func TestConstantValidIDRejected(t *testing.T) {
	constant := strings.Repeat("a", 32)
	last := Ticket{ID: constant, Title: "fixed", Status: "open"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"instance_id": "test", "release_version": releaseVersion, "source_revision": sourceRevision})
		case "/health", "/ready":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/tickets":
			if r.Method == "POST" {
				var b map[string]string
				json.NewDecoder(r.Body).Decode(&b)
				last.Title = b["title"]
				w.WriteHeader(201)
				json.NewEncoder(w).Encode(last)
			} else {
				json.NewEncoder(w).Encode(map[string]any{"items": []Ticket{last}, "total": 1})
			}
		default:
			json.NewEncoder(w).Encode(last)
		}
	}))
	defer srv.Close()
	r := Report{Checks: []Check{}}
	c := Checker{srv.URL, "test", releaseVersion, sourceRevision, srv.Client(), &r}
	c.httpChecks(Profile{Mandatory: []string{"target", "version", "ticket.create", "ticket.read"}}, "")
	failed := false
	for _, ch := range r.Checks {
		if ch.Status == "fail" {
			failed = true
		}
	}
	if !failed {
		t.Fatal("a constant syntactically valid 32-char ID passed uniqueness")
	}
}
func TestMalformedStoreAndSingleWriter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tickets.json"), []byte(`{"broken":`), 0600)
	if s, e := openStore("file", dir); e == nil {
		s.close()
		t.Fatal("malformed store silently accepted")
	}
	dir = t.TempDir()
	first, e := openStore("file", dir)
	if e != nil {
		t.Fatal(e)
	}
	defer first.close()
	// Windows uses its platform lock implementation; Linux is also tested on host.
	if runtimeLockSupported() {
		if second, e := openStore("file", dir); e == nil {
			second.close()
			t.Fatal("second writer accepted")
		}
	}
}
func TestConcurrentIdempotency(t *testing.T) {
	s, e := openStore("file", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.close()
	ids := make(chan string, 12)
	errors := make(chan error, 12)
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticket, _, e := s.create("simultaneous", "one-key")
			if e != nil {
				errors <- e
			} else {
				ids <- ticket.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errors)
	for e := range errors {
		t.Fatal(e)
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("same key created duplicates")
		}
		id = got
	}
}
func TestInstancePathRejectsEscape(t *testing.T) {
	for _, id := range []string{"../peer", "/tmp/peer", "a;touch-x", "x"} {
		if _, e := ownedPath(t.TempDir(), id); e == nil {
			t.Fatal("unsafe instance accepted", id)
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	if os.Symlink(outside, filepath.Join(root, "my-instance")) == nil {
		if _, e := ownedPath(root, "my-instance"); e == nil {
			t.Fatal("symlink instance accepted")
		}
	}
}
func TestBaselineMetadataRejectsDamage(t *testing.T) {
	b := Baseline{SchemaVersion: "1.0", Profile: "m06-service", Instance: "test", Created: now(), Tickets: []Ticket{{ID: strings.Repeat("b", 32), Title: "old", Status: "open"}}, ProfileVersion: "1.0", SourceRevision: strings.Repeat("a", 64), Seed: 417}
	p := filepath.Join(t.TempDir(), "baseline.json")
	atomicJSON(p, b, false)
	if _, e := loadBaseline(p, "test"); e != nil {
		t.Fatal(e)
	}
	b.Seed = 0
	atomicJSON(p, b, false)
	if _, e := loadBaseline(p, "test"); e == nil {
		t.Fatal("seedless baseline accepted")
	}
}

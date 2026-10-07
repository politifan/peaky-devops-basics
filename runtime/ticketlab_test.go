package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTicketContractPersistence(t *testing.T) {
	dir := t.TempDir()
	s, e := openStore("file", dir)
	defer s.close()
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(api(s, "test", "file"))
	defer server.Close()
	call := func(method, path, body, key string) (int, []byte) {
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Idempotency-Key", key)
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b
	}
	code, b := call("POST", "/tickets", `{"title":"  Первая заявка  "}`, "key1")
	if code != 201 {
		t.Fatalf("POST %d %s", code, b)
	}
	var ticket Ticket
	json.Unmarshal(b, &ticket)
	if ticket.Title != "Первая заявка" {
		t.Fatal(ticket)
	}
	code, b = call("POST", "/tickets", `{"title":"Первая заявка"}`, "key1")
	var repeated Ticket
	json.Unmarshal(b, &repeated)
	if code != 200 || repeated != ticket {
		t.Fatal("retry", code, string(b))
	}
	code, _ = call("POST", "/tickets", `{"title":"Другая"}`, "key1")
	if code != 409 {
		t.Fatal("conflict", code)
	}
	for _, body := range []string{`{"title":" "}`, `{"title":"valid","extra":true}`, `not json`, `{"title":"x"}{}`} {
		code, _ = call("POST", "/tickets", body, "")
		if code != 400 {
			t.Fatal("invalid", body, code)
		}
	}
	code, _ = call("POST", "/tickets", `{"title":"`+strings.Repeat("x", 5000)+`"}`, "")
	if code != 413 {
		t.Fatal("size", code)
	}
	code, b = call("PATCH", "/tickets/"+ticket.ID, `{"status":"closed"}`, "")
	if code != 200 {
		t.Fatal(code, string(b))
	}
	ticket.Status = "closed"
	s.close()
	s2, e := openStore("file", dir)
	defer s2.close()
	if e != nil {
		t.Fatal(e)
	}
	got, e := s2.get(ticket.ID)
	if e != nil || got != ticket {
		t.Fatal("reopen lost old data", got, e)
	}
	again, retry, e := s2.create(ticket.Title, "key1")
	if e != nil || !retry || again != ticket {
		t.Fatal("reopen idempotency", again, e)
	}
	_ = filepath.Join(dir, "tickets.json")
}
func TestCheckerGoodAndBroken(t *testing.T) {
	oldDefect := defect
	defer func() { defect = oldDefect }()
	var profiles map[string]Profile
	json.Unmarshal(profileBytes, &profiles)
	for _, variant := range []string{"none", "bad-body", "bad-write", "bad-retry"} {
		t.Run(variant, func(t *testing.T) {
			defect = variant
			s, _ := openStore("file", t.TempDir())
			defer s.close()
			srv := httptest.NewServer(api(s, "test", "file"))
			defer srv.Close()
			r := Report{Checks: []Check{}}
			c := Checker{srv.URL, "test", releaseVersion, sourceRevision, srv.Client(), &r}
			p := profiles["m03-http"]
			if variant == "bad-retry" {
				p = profiles["m12-retry"]
			}
			c.httpChecks(p, "")
			fail := false
			for _, ch := range r.Checks {
				if ch.Status == "fail" {
					fail = true
				}
			}
			if variant == "none" && fail {
				t.Fatalf("good failed %+v", r.Checks)
			}
			if variant != "none" && !fail {
				t.Fatalf("bad passed %+v", r.Checks)
			}
		})
	}
}
func TestCheckerWrongJSONAndRedirect(t *testing.T) {
	for _, body := range []string{"not-json", "<html></html>", strings.Repeat("x", 65537), `{"id":"constant","title":"constant"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}))
		report := Report{Checks: []Check{}}
		c := Checker{srv.URL, "test", releaseVersion, sourceRevision, srv.Client(), &report}
		var ps map[string]Profile
		json.Unmarshal(profileBytes, &ps)
		c.httpChecks(ps["m03-http"], "")
		fail := false
		for _, ch := range report.Checks {
			if ch.Status == "fail" {
				fail = true
			}
		}
		srv.Close()
		if !fail {
			t.Fatal("malformed fixture passed")
		}
	}
}

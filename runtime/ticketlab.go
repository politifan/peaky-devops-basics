package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, e string) {
	reply(w, status, map[string]string{"error": e})
}
func input(w http.ResponseWriter, r *http.Request, v any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		problem(w, 415, "content_type_required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		var max *http.MaxBytesError
		if errors.As(e, &max) {
			problem(w, 413, "body_too_large")
		} else {
			problem(w, 400, "invalid_json")
		}
		return false
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		problem(w, 400, "invalid_json")
		return false
	}
	return true
}
func api(s *Store, instance, kind string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"api_contract": "1.0", "release_version": releaseVersion, "source_revision": sourceRevision, "storage": kind, "instance_id": instance})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if defect == "bad-body" {
			reply(w, 200, map[string]string{"status": "wrong"})
			return
		}
		reply(w, 200, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if s.ready() != nil {
			problem(w, 503, "dependency_unavailable")
			return
		}
		reply(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /tickets", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Title string `json:"title"`
		}
		if !input(w, r, &b) {
			return
		}
		b.Title = strings.TrimSpace(b.Title)
		n := utf8.RuneCountInString(b.Title)
		if n < 1 || n > 120 {
			problem(w, 400, "invalid_title")
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if len(key) > 128 {
			problem(w, 400, "invalid_key")
			return
		}
		for _, c := range key {
			if c < 33 || c > 126 {
				problem(w, 400, "invalid_key")
				return
			}
		}
		if defect == "bad-write" {
			problem(w, 503, "storage_unavailable")
			return
		}
		if defect == "bad-retry" {
			key = ""
		}
		t, repeated, e := s.create(b.Title, key)
		if e != nil {
			code := 503
			if e == conflict {
				code = 409
			}
			problem(w, code, storageError(e))
			return
		}
		code := 201
		if repeated {
			code = 200
		}
		reply(w, code, t)
	})
	mux.HandleFunc("GET /tickets/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, e := s.get(r.PathValue("id"))
		if e != nil {
			code := 503
			if e == notfound {
				code = 404
			}
			problem(w, code, storageError(e))
			return
		}
		if defect == "bad-body" {
			t.Title = "подменённое значение"
		}
		reply(w, 200, t)
	})
	mux.HandleFunc("PATCH /tickets/{id}", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Status string `json:"status"`
		}
		if !input(w, r, &b) {
			return
		}
		if b.Status != "open" && b.Status != "closed" {
			problem(w, 400, "invalid_status")
			return
		}
		t, e := s.update(r.PathValue("id"), b.Status)
		if e != nil {
			code := 503
			if e == notfound {
				code = 404
			}
			problem(w, code, storageError(e))
			return
		}
		reply(w, 200, t)
	})
	mux.HandleFunc("GET /tickets", func(w http.ResponseWriter, r *http.Request) {
		limit, offset := 20, 0
		var e error
		if v := r.URL.Query().Get("limit"); v != "" {
			limit, e = strconv.Atoi(v)
		}
		if e != nil || limit < 1 || limit > 100 {
			problem(w, 400, "invalid_limit")
			return
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			offset, e = strconv.Atoi(v)
		}
		if e != nil || offset < 0 {
			problem(w, 400, "invalid_offset")
			return
		}
		list, total, e := s.list(limit, offset)
		if e != nil {
			problem(w, 503, "storage_unavailable")
			return
		}
		reply(w, 200, map[string]any{"items": list, "total": total})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { problem(w, 404, "not_found") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		log.Printf("instance=%s method=%s path=%s", instance, r.Method, r.URL.Path)
	})
}
func ticketMain(args []string) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("ticketlab version | serve --listen ADDRESS --data-dir ABSOLUTE --instance ID --storage file|postgresql | migrate --check|--apply")
		return 0
	}
	if args[0] == "version" {
		fmt.Printf("TicketLab %s revision %s API 1.0\n", releaseVersion, sourceRevision)
		return 0
	}
	if args[0] == "migrate" {
		f := flag.NewFlagSet("migrate", flag.ContinueOnError)
		apply := f.Bool("apply", false, "применить schema 1 без удаления данных")
		check := f.Bool("check", false, "проверить schema")
		if f.Parse(args[1:]) != nil || *apply == *check {
			return 64
		}
		s, e := openStore("postgresql", "")
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		defer s.close()
		if e = s.migrate(*apply); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 1
		}
		fmt.Println("schema revision 1 подтверждена")
		return 0
	}
	if args[0] != "serve" {
		return 64
	}
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := f.String("listen", "127.0.0.1:18003", "адрес listener")
	dir := f.String("data-dir", "", "абсолютный каталог файловых данных")
	instance := f.String("instance", "", "учебный instance")
	kind := f.String("storage", "file", "file или postgresql")
	if f.Parse(args[1:]) != nil || *instance == "" {
		return 64
	}
	s, e := openStore(*kind, *dir)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	defer s.close()
	srv := &http.Server{Addr: *listen, Handler: api(s, *instance, *kind), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sig
		ctx, c := context.WithTimeout(context.Background(), 4*time.Second)
		defer c()
		srv.Shutdown(ctx)
	}()
	log.Printf("TicketLab release=%s instance=%s storage=%s listen=%s", releaseVersion, *instance, *kind, *listen)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "listener недоступен:", e)
		return 2
	}
	return 0
}

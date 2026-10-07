package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var toolName = "ticketlab"
var releaseVersion = "3.0.0-good"
var sourceRevision = "unbuilt"
var defect = "none"

func marker() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func atomicJSON(path string, value any, exclusive bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if exclusive {
		if _, e := os.Stat(path); e == nil {
			return fmt.Errorf("baseline уже существует: новая попытка требует нового файла")
		}
	}
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".partial-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if exclusive {
		return os.Link(tmp, path)
	}
	return os.Rename(tmp, path)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func main() {
	name := toolName
	if strings.Contains(filepath.Base(os.Args[0]), "labcheck") {
		name = "labcheck"
	}
	if strings.Contains(filepath.Base(os.Args[0]), "labctl") {
		name = "labctl"
	}
	code := 64
	switch name {
	case "ticketlab":
		code = ticketMain(os.Args[1:])
	case "labcheck":
		code = checkMain(os.Args[1:])
	case "labctl":
		code = ctlMain(os.Args[1:])
	case "labworker":
		code = workerMain(os.Args[1:])
	}
	os.Exit(code)
}

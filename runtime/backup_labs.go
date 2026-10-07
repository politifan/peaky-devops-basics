package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func backupControl(command, p string, m Instance) int {
	if m.ModuleID != "m10" && m.ModuleID != "m11" {
		fmt.Fprintln(os.Stderr, "backup/restore доступны в модулях 10 и 11")
		return 64
	}
	pg := "peaky301758-" + m.InstanceID + "-db-1"
	if _, e := inspectOwned(pg, m.InstanceID); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	dumpPath := filepath.Join(p, "state/backup.dump")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if command == "backup" {
		if _, e := os.Stat(dumpPath); e == nil {
			fmt.Fprintln(os.Stderr, "backup уже существует; не перезаписываем")
			return 64
		}
		dump, e := os.CreateTemp(filepath.Join(p, "state"), "backup-*.partial")
		if e != nil {
			fmt.Fprintln(os.Stderr, "backup уже существует или путь недоступен; не перезаписываем")
			return 64
		}
		defer dump.Close()
		partialPath := dump.Name()
		if e = composeRun(p, m, "stop", "--timeout", "10", "api"); e != nil {
			return 2
		}
		defer composeRun(p, m, "start", "api")
		snapshot, e := pgSnapshot(pg, m.InstanceID, "ticketlab")
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		cmd := exec.CommandContext(ctx, "docker", "exec", pg, "pg_dump", "-U", "student", "-d", "ticketlab", "-Fc")
		cmd.Stdout = dump
		cmd.Stderr = os.Stderr
		if e = cmd.Run(); e != nil {
			fmt.Fprintln(os.Stderr, "pg_dump не завершён; частичный файл не является backup")
			return 2
		}
		if e = dump.Sync(); e != nil {
			return 2
		}
		dump.Close()
		if e = os.Link(partialPath, dumpPath); e != nil {
			fmt.Fprintln(os.Stderr, "готовый dump не опубликован; partial сохранён")
			return 2
		}
		os.Remove(partialPath)
		content, e := os.ReadFile(dumpPath)
		if e != nil {
			return 2
		}
		sum := sha256.Sum256(content)
		if e = atomicJSON(filepath.Join(p, "state/backup.json"), map[string]any{"course_id": 301758, "instance_id": m.InstanceID, "created_utc": now(), "dump_sha256": hex.EncodeToString(sum[:]), "snapshot": snapshot, "snapshot_consistency": "only assigned API stopped; no other writers in this exercise"}, true); e != nil {
			return 2
		}
		fmt.Println("backup готов:", dumpPath, "sha256", hex.EncodeToString(sum[:]), "rows", len(snapshot.Rows))
		return 0
	}
	if command != "restore" {
		return 64
	}
	metadata, e := os.ReadFile(filepath.Join(p, "state/backup.json"))
	var backup map[string]json.RawMessage
	if e != nil || json.Unmarshal(metadata, &backup) != nil {
		fmt.Fprintln(os.Stderr, "готовый manifest backup отсутствует; restore не начинается")
		return 2
	}
	var expectedHash string
	if json.Unmarshal(backup["dump_sha256"], &expectedHash) != nil || len(expectedHash) != 64 {
		return 2
	}
	bytes, e := os.ReadFile(dumpPath)
	if e != nil {
		return 2
	}
	hash := sha256.Sum256(bytes)
	if hex.EncodeToString(hash[:]) != expectedHash || len(bytes) < 5 || string(bytes[:5]) != "PGDMP" {
		fmt.Fprintln(os.Stderr, "checksum/формат dump не совпали; БД не изменяются")
		return 1
	}
	restoredDB := "restored_" + strings.ReplaceAll(m.InstanceID, "-", "_")
	dump, e := os.Open(dumpPath)
	if e != nil {
		fmt.Fprintln(os.Stderr, "backup не найден")
		return 2
	}
	defer dump.Close()
	// A new database is required. createdb fails if the destination already exists.
	cmd := exec.CommandContext(ctx, "docker", "exec", pg, "createdb", "-U", "postgres", "-O", "student", restoredDB)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Run(); e != nil {
		fmt.Fprintln(os.Stderr, "новая БД не создана; существующая не перезаписывается")
		return 2
	}
	cmd = exec.CommandContext(ctx, "docker", "exec", "-i", pg, "pg_restore", "-U", "student", "-d", restoredDB, "--no-owner", "--exit-on-error")
	cmd.Stdin = dump
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Run(); e != nil {
		fmt.Fprintln(os.Stderr, "restore не завершён; исходная БД сохранена")
		return 2
	}
	// Read this instance's own protected URL, without logging its password.
	env, e := os.ReadFile(filepath.Join(p, ".env"))
	if e != nil {
		return 2
	}
	url := ""
	for _, line := range strings.Split(string(env), "\n") {
		if strings.HasPrefix(line, "TICKETLAB_DATABASE_URL=") {
			url = strings.TrimPrefix(line, "TICKETLAB_DATABASE_URL=")
		}
	}
	if !strings.Contains(url, "/ticketlab?") {
		fmt.Fprintln(os.Stderr, "неожиданный адрес учебной БД")
		return 2
	}
	url = strings.Replace(url, "/ticketlab?", "/"+restoredDB+"?", 1)
	restoreEnv := filepath.Join(p, "config/restore.env")
	if e = os.WriteFile(restoreEnv, []byte("TICKETLAB_DATABASE_URL="+url+"\n"), 0600); e != nil {
		return 2
	}
	name := "peaky301758-" + m.InstanceID + "-restore"
	image := "peaky301758-" + m.InstanceID + ":3.0.0"
	args := []string{"run", "-d", "--name", name, "--label", "io.peaky.course=301758", "--label", "io.peaky.instance=" + m.InstanceID, "--memory", "96m", "--cpus", "0.25", "--pids-limit", "64", "--user", "10001", "--network", "peaky301758-" + m.InstanceID + "_default", "--env-file", restoreEnv, "-p", fmt.Sprintf("127.0.0.1:%d:18000", m.Port+100), image, "serve", "--listen", "0.0.0.0:18000", "--data-dir", "/data", "--instance", m.InstanceID, "--storage", "postgresql"}
	cmd = exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Run(); e != nil {
		return 2
	}
	if e = atomicJSON(filepath.Join(p, "state/restore.json"), map[string]any{"created_utc": now(), "source_database": "ticketlab", "restore_database": restoredDB, "restore_container": name, "restore_url": fmt.Sprintf("http://127.0.0.1:%d", m.Port+100), "scope": "local metadata; checker repeats actual SQL and HTTP"}, true); e != nil {
		return 2
	}
	fmt.Println("restore создан в другой БД:", restoredDB, "HTTP порт", m.Port+100)
	return 0
}

func copyStream(dst string, reader io.Reader) error {
	f, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = io.Copy(f, reader)
	return e
}

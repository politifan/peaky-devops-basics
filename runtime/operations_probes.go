package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PGRow struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Key    string `json:"key"`
}
type PGSnapshot struct {
	Database string  `json:"database"`
	OID      int     `json:"oid"`
	Schema   int     `json:"schema"`
	Rows     []PGRow `json:"rows"`
}

func pgSnapshot(container, instance, database string) (PGSnapshot, error) {
	if _, e := inspectOwned(container, instance); e != nil {
		return PGSnapshot{}, e
	}
	sql := `SELECT json_build_object('database',current_database(),'oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),'schema',(SELECT revision FROM ticketlab_schema WHERE singleton),'rows',COALESCE((SELECT json_agg(json_build_object('id',id,'title',title,'status',status,'key',idempotency_key) ORDER BY id) FROM tickets),'[]'::json));`
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	b, e := probeCommand(ctx, "/usr/bin/docker", "exec", container, "psql", "-U", "student", "-d", database, "-Atc", sql).Output()
	if e != nil {
		return PGSnapshot{}, fmt.Errorf("SQL snapshot недоступен")
	}
	var s PGSnapshot
	if e = json.Unmarshal(b, &s); e != nil || s.Database != database || s.OID == 0 || s.Schema != 1 {
		return s, fmt.Errorf("SQL snapshot/schema не подтверждены")
	}
	return s, nil
}
func rowsInclude(actual, expected []PGRow) bool {
	rows := map[string]PGRow{}
	for _, r := range actual {
		rows[r.ID] = r
	}
	for _, r := range expected {
		if rows[r.ID] != r {
			return false
		}
	}
	return true
}

func (c *Checker) restoreProbes(p Profile, baselinePath, container, sourceDB, restoreDB string) {
	if !has(p, "restore.snapshot") || has(p, "project.clean_room") {
		return
	}
	baseline, e := loadBaseline(baselinePath, c.instance)
	if e != nil || baseline.Postgres == nil {
		c.r.Checks = append(c.r.Checks, Check{"restore.snapshot", "not_checked", "SQL snapshot до backup", "не предоставлен", "Сохраните авторский baseline до backup с --postgres-container"})
		return
	}
	source, se := pgSnapshot(container, c.instance, sourceDB)
	restored, re := pgSnapshot(container, c.instance, restoreDB)
	if has(p, "restore.identity") {
		c.add("restore.identity", se == nil && re == nil && source.Database != restored.Database && source.OID != restored.OID && restored.OID != baseline.Postgres.OID, "другая БД и другой OID", map[string]any{"source_db": source.Database, "source_oid": source.OID, "restore_db": restored.Database, "restore_oid": restored.OID, "source_error": errorText(se), "restore_error": errorText(re)}, "Восстановление поверх исходной БД не является приёмкой")
	}
	c.add("restore.snapshot", re == nil && restored.Schema == baseline.Postgres.Schema && len(restored.Rows) == len(baseline.Postgres.Rows) && rowsInclude(restored.Rows, baseline.Postgres.Rows), map[string]any{"rows": len(baseline.Postgres.Rows), "schema": baseline.Postgres.Schema, "all_fields_equal": true}, map[string]any{"rows": len(restored.Rows), "schema": restored.Schema, "error": errorText(re)}, "Сравниваются все исходные строки, включая ключи повторов; поздние строки не входят в backup")
	if has(p, "restore.source_preserved") {
		c.add("restore.source_preserved", se == nil && source.OID == baseline.Postgres.OID && source.Schema == baseline.Postgres.Schema && rowsInclude(source.Rows, baseline.Postgres.Rows), "исходная БД и её прежние данные сохранены", map[string]any{"oid": source.OID, "rows": len(source.Rows), "error": errorText(se)}, "Проверьте источник отдельно от восстановленного приложения")
	}
	// Check every restored baseline row through the target API, not only the generated HTTP sentinel.
	for _, row := range baseline.Postgres.Rows {
		var t Ticket
		s, e := c.json("GET", "/tickets/"+row.ID, nil, "", &t)
		c.add("restore.http."+row.ID, e == nil && s == 200 && t.ID == row.ID && t.Title == row.Title && t.Status == row.Status, "HTTP читает ту же восстановленную строку", map[string]any{"status": s, "ticket": t, "error": errorText(e)}, "SQL snapshot и HTTP endpoint должны относиться к одной новой БД")
	}
}

func (c *Checker) cleanRoomProbes(p Profile, baselinePath, sourceContainer, sourceDB, peerContainer, peerInstance, peerDB, peerURL string) {
	if !has(p, "project.clean_room") {
		return
	}
	baseline, e := loadBaseline(baselinePath, c.instance)
	if e != nil || baseline.Postgres == nil || peerContainer == "" || peerInstance == "" || peerURL == "" {
		return
	}
	original, oe := inspectOwned(sourceContainer, c.instance)
	peer, pe := inspectOwned(peerContainer, peerInstance)
	ps, se := pgSnapshot(peerContainer, peerInstance, peerDB)
	sourceVolume := volumeReference(original, "/var/lib/postgresql/data")
	peerVolume := volumeReference(peer, "/var/lib/postgresql/data")
	c.add("project.clean_room", oe == nil && pe == nil && se == nil && peerInstance != c.instance && peer.ID != original.ID && peerVolume != "" && sourceVolume != "" && peerVolume != sourceVolume, "другой instance контейнер и PG volume", map[string]any{"source_instance": c.instance, "peer_instance": peerInstance, "source_volume": sourceVolume, "peer_volume": peerVolume, "error": errorText(se)}, "Повторите передачу в новый собственный проект; два порта одной БД не являются clean-room")
	same := se == nil && ps.Schema == baseline.Postgres.Schema && len(ps.Rows) == len(baseline.Postgres.Rows) && rowsInclude(ps.Rows, baseline.Postgres.Rows)
	// Peer URL is checked against the same loopback-only policy as other trusted targets.
	if !safeBaseURL(peerURL) {
		c.add("restore.snapshot", false, "назначенный loopback peer URL", "небезопасный адрес", "Используйте адрес назначенного экземпляра")
		return
	}
	pc := Checker{peerURL, peerInstance, c.expectedRelease, c.expectedSource, c.client, &Report{Checks: []Check{}}}
	var version map[string]string
	status, ve := pc.json("GET", "/version", nil, "", &version)
	same = same && ve == nil && status == 200 && version["instance_id"] == peerInstance && version["source_revision"] == c.expectedSource && version["release_version"] == c.expectedRelease
	for _, row := range baseline.Postgres.Rows {
		var ticket Ticket
		status, e := pc.json("GET", "/tickets/"+row.ID, nil, "", &ticket)
		same = same && e == nil && status == 200 && ticket.ID == row.ID && ticket.Title == row.Title && ticket.Status == row.Status
	}
	c.add("restore.snapshot", same, "точная schema все строки и HTTP-чтение на новом instance", map[string]any{"rows": len(ps.Rows), "schema": ps.Schema, "version": version, "error": errorText(se)}, "Переданный дамп должен восстановиться и читаться на втором стенде")
}

func (c *Checker) ciProbes(p Profile, workspace string) {
	if !has(p, "ci.artifact") && !has(p, "ci.mandatory_failure") {
		return
	}
	config, e := probeReadFile(filepath.Join(workspace, "config/ci.env"))
	if e != nil {
		return
	}
	settings := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(config)), "\n") {
		k, v, found := strings.Cut(line, "=")
		if found {
			settings[k] = v
		}
	}
	// The declared source is compared to the assignment's known source, never taken from the live service.
	if has(p, "ci.artifact") {
		c.add("ci.artifact", settings["artifact-source"] == c.expectedSource, c.expectedSource, settings["artifact-source"], "Свяжите артефакт с source_revision из известного manifest; tag latest не доказывает версию")
	}
	if !has(p, "ci.mandatory_failure") {
		return
	}
	if settings["mandatory-check"] != "on" {
		c.add("ci.mandatory_failure", false, "обязательная HTTP стадия включена", settings["mandatory-check"], "Успех pipeline с отключённой обязательной проверкой не засчитывается")
		return
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return
	}
	addr := listener.Addr().String()
	listener.Close()
	tmp, e := os.MkdirTemp("", "peaky-ci-negative-")
	if e != nil {
		return
	}
	defer os.RemoveAll(tmp)
	executable, _ := os.Executable()
	cmd := exec.Command(filepath.Join(filepath.Dir(executable), "ticketlab-bad-write"), "serve", "--listen", addr, "--data-dir", tmp, "--instance", c.instance+"-ci-negative")
	log, e := os.Create(filepath.Join(tmp, "service.log"))
	if e != nil {
		return
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		return
	}
	defer func() { cmd.Process.Signal(os.Interrupt); cmd.Process.Kill(); cmd.Wait() }()
	negativeReport := Report{Checks: []Check{}}
	nc := Checker{"http://" + addr, c.instance + "-ci-negative", "3.0.0-bad-write", sourceRevision, &http.Client{Timeout: 5 * time.Second}, &negativeReport}
	for n := 0; n < 40; n++ {
		resp, err := nc.client.Get(nc.base + "/version")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	nc.httpChecks(Profile{Mandatory: []string{"target", "version", "health", "ready", "ticket.create", "ticket.read"}}, "")
	expectedFailure, readyPassed, versionPassed := false, false, false
	for _, check := range negativeReport.Checks {
		if check.ID == "ticket.create" && check.Status == "fail" {
			expectedFailure = true
		}
		if check.ID == "ready" && check.Status == "pass" {
			readyPassed = true
		}
		if check.ID == "version" && check.Status == "pass" {
			versionPassed = true
		}
	}
	c.add("ci.mandatory_failure", expectedFailure && readyPassed && versionPassed, "реальная обязательная проверка отклоняет готовый дефект записи", negativeReport.Checks, "Проверяется запуск известного bad-write и настоящий POST; студентский passed=true не принимается")
}

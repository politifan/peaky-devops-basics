package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed profiles.json
var profileBytes []byte

type Profile struct {
	Version   string   `json:"version"`
	Kind      string   `json:"kind"`
	Mandatory []string `json:"mandatory"`
}
type Check struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Expected any    `json:"expected"`
	Actual   any    `json:"actual"`
	Hint     string `json:"hint,omitempty"`
}
type Report struct {
	SchemaVersion  string   `json:"schema_version"`
	ToolVersion    string   `json:"tool_version"`
	ProfileID      string   `json:"profile_id"`
	ProfileVersion string   `json:"profile_version"`
	InstanceID     string   `json:"instance_id"`
	AttemptID      string   `json:"attempt_id"`
	VariantSeed    int      `json:"variant_seed"`
	Scope          string   `json:"scope"`
	Started        string   `json:"started_utc"`
	Ended          string   `json:"ended_utc"`
	Checks         []Check  `json:"checks"`
	Overall        string   `json:"overall"`
	ExitCode       int      `json:"exit_code"`
	Limits         []string `json:"limits"`
}
type Baseline struct {
	SchemaVersion  string      `json:"schema_version"`
	Profile        string      `json:"profile_id"`
	Instance       string      `json:"instance_id"`
	Created        string      `json:"created_utc"`
	Tickets        []Ticket    `json:"tickets"`
	ContainerID    string      `json:"container_id,omitempty"`
	Volume         string      `json:"volume,omitempty"`
	ProfileVersion string      `json:"profile_version,omitempty"`
	SourceRevision string      `json:"source_revision,omitempty"`
	Seed           int         `json:"variant_seed,omitempty"`
	Postgres       *PGSnapshot `json:"postgres_snapshot,omitempty"`
}
type Checker struct {
	base, instance, expectedRelease, expectedSource string
	client                                          *http.Client
	r                                               *Report
}

func (c *Checker) add(id string, pass bool, expected, actual any, hint string) {
	status := "fail"
	if pass {
		status = "pass"
	}
	c.r.Checks = append(c.r.Checks, Check{id, status, expected, actual, hint})
}
func (c *Checker) request(method, path string, body any, key string) (int, []byte, error) {
	var payload []byte
	var e error
	if body != nil {
		payload, e = json.Marshal(body)
		if e != nil {
			return 0, nil, e
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
	if e != nil {
		return 0, nil, e
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, e := c.client.Do(req)
	if e != nil {
		return 0, nil, fmt.Errorf("учебный endpoint не отвечает")
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if len(b) > 65536 {
		return response.StatusCode, nil, fmt.Errorf("тело ответа превышает 64 KiB")
	}
	if e != nil {
		return response.StatusCode, nil, e
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return response.StatusCode, nil, fmt.Errorf("ожидался Content-Type application/json")
	}
	if !json.Valid(b) {
		return response.StatusCode, nil, fmt.Errorf("тело не является JSON")
	}
	return response.StatusCode, b, nil
}
func (c *Checker) json(method, path string, body any, key string, out any) (int, error) {
	status, b, e := c.request(method, path, body, key)
	if e != nil {
		return status, e
	}
	e = json.Unmarshal(b, out)
	return status, e
}
func has(p Profile, id string) bool {
	for _, x := range p.Mandatory {
		if x == id {
			return true
		}
	}
	return false
}
func (c *Checker) httpChecks(p Profile, state string) {
	var v map[string]string
	status, e := c.json("GET", "/version", nil, "", &v)
	c.add("target", e == nil && status == 200 && v["instance_id"] == c.instance, "назначенный instance "+c.instance, map[string]any{"status": status, "instance": v["instance_id"], "error": errorText(e)}, "Сверьте адрес и instance в запуске приложения")
	c.add("version", e == nil && status == 200 && v["release_version"] == c.expectedRelease && v["source_revision"] == c.expectedSource && v["api_contract"] == "1.0", map[string]string{"release": c.expectedRelease, "source": c.expectedSource, "api": "1.0"}, v, "Сверьте поставку с manifest, а не с текущим /version")
	for _, endpoint := range []struct{ id, path, want string }{{"health", "/health", "alive"}, {"ready", "/ready", "ready"}} {
		if has(p, endpoint.id) {
			var b map[string]string
			s, e := c.json("GET", endpoint.path, nil, "", &b)
			c.add(endpoint.id, e == nil && s == 200 && b["status"] == endpoint.want, "200 + status="+endpoint.want, map[string]any{"status": s, "body": b, "error": errorText(e)}, "Проверьте журнал и объявленную зависимость; 200 без нужного тела недостаточно")
		}
	}
	if has(p, "ticket.create") || has(p, "ticket.read") || has(p, "ticket.retry") || has(p, "ticket.conflict") {
		title := "lab-" + marker()
		key := "check-" + marker()
		var t Ticket
		s, e := c.json("POST", "/tickets", map[string]string{"title": title}, key, &t)
		ok := e == nil && s == 201 && t.Title == title && t.Status == "open" && len(t.ID) == 32
		c.add("ticket.create", ok, "201 + новая заявка с title="+title, map[string]any{"status": s, "ticket": t, "error": errorText(e)}, "Проверьте /ready и журнал операции POST")
		if !ok {
			for _, dependent := range []string{"ticket.read", "ticket.retry", "ticket.conflict"} {
				if has(p, dependent) {
					c.add(dependent, false, "проверка созданной новой заявки", "не выполнялась: обязательный POST не подтвердил создание", "Сначала устраните отказ ticket.create; это блокировка последующей операции")
				}
			}
		}
		if ok {
			var read Ticket
			rs, re := c.json("GET", "/tickets/"+t.ID, nil, "", &read)
			c.add("ticket.read", re == nil && rs == 200 && read == t, t, map[string]any{"status": rs, "ticket": read, "error": errorText(re)}, "Сопоставьте URL записи и чтения; проверьте каталог/БД")
			var second Ticket
			secondTitle := "unique-" + marker()
			us, ue := c.json("POST", "/tickets", map[string]string{"title": secondTitle}, "unique-"+marker(), &second)
			var reread Ticket
			fs, fe := c.json("GET", "/tickets/"+t.ID, nil, "", &reread)
			c.add("ticket.unique", ue == nil && us == 201 && second.Title == secondTitle && len(second.ID) == 32 && second.ID != t.ID && fe == nil && fs == 200 && reread == t, "две новые заявки имеют разные ID и первая не перезаписана", map[string]any{"first": t.ID, "second": second.ID, "first_after": reread, "status": us}, "Постоянный ID или перезапись последней заявки не соответствуют контракту")
			if has(p, "ticket.retry") {
				var dup Ticket
				ds, de := c.json("POST", "/tickets", map[string]string{"title": " " + title + " "}, key, &dup)
				c.add("ticket.retry", de == nil && ds == 200 && dup == t, "200 + тот же ID и данные", map[string]any{"status": ds, "ticket": dup}, "Повтор использует тот же ключ и нормализованное тело; сверьте релиз")
			}
			if has(p, "ticket.conflict") {
				var b map[string]any
				cs, ce := c.json("POST", "/tickets", map[string]string{"title": title + "-changed"}, key, &b)
				var after Ticket
				as, ae := c.json("GET", "/tickets/"+t.ID, nil, "", &after)
				c.add("ticket.conflict", ce == nil && cs == 409 && ae == nil && as == 200 && after == t, "409 + прежняя заявка не изменилась", map[string]any{"status": cs, "after": after}, "Тот же ключ с другим title обязан отказать, а не создавать ещё одну запись")
			}
		}
	}
	if has(p, "ticket.invalid") {
		var before struct {
			Total int `json:"total"`
		}
		beforeStatus, be := c.json("GET", "/tickets?limit=1", nil, "", &before)
		var invalid map[string]any
		is, ie := c.json("POST", "/tickets", map[string]string{"title": "   "}, "", &invalid)
		var after struct {
			Total int `json:"total"`
		}
		afterStatus, ae := c.json("GET", "/tickets?limit=1", nil, "", &after)
		c.add("ticket.invalid", be == nil && beforeStatus == 200 && ie == nil && ae == nil && afterStatus == 200 && is == 400 && before.Total == after.Total, "400 без новой записи при доступном чтении счётчика", map[string]any{"status": is, "before_status": beforeStatus, "after_status": afterStatus, "before": before.Total, "after": after.Total}, "Пустой title не должен попасть в хранилище; проверка счётчика предполагает отсутствие параллельных пишущих клиентов")
	}
	if has(p, "ticket.not_found") {
		var b map[string]any
		ns, ne := c.json("GET", "/tickets/"+marker(), nil, "", &b)
		c.add("ticket.not_found", ne == nil && ns == 404, "404", map[string]any{"status": ns, "body": b}, "Неизвестный ID не должен получать готовую успешную запись")
	}
	if has(p, "old_data") {
		b, e := loadBaseline(state, c.instance)
		if e != nil {
			c.r.Checks = append(c.r.Checks, Check{"old_data", "not_checked", "baseline до операции", errorText(e), "Укажите сохранённый baseline этой попытки; не создавайте новый после потери"})
		} else {
			pass := true
			observed := []Ticket{}
			for _, old := range b.Tickets {
				var got Ticket
				gs, ge := c.json("GET", "/tickets/"+old.ID, nil, "", &got)
				if ge != nil || gs != 200 || got != old {
					pass = false
				}
				observed = append(observed, got)
			}
			c.add("old_data", pass, b.Tickets, observed, "Проверьте прежний каталог/volume и соответствие instance; новый пустой baseline не является восстановлением")
		}
	}
}
func errorText(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
func loadBaseline(path, instance string) (Baseline, error) {
	var b Baseline
	data, e := os.ReadFile(path)
	if e != nil {
		return b, fmt.Errorf("baseline недоступен")
	}
	if e = json.Unmarshal(data, &b); e != nil || b.SchemaVersion != "1.0" || b.Instance != instance || len(b.Tickets) == 0 {
		return b, fmt.Errorf("baseline не соответствует instance/schema")
	}
	var profiles map[string]Profile
	_ = json.Unmarshal(profileBytes, &profiles)
	profile, exists := profiles[b.Profile]
	_, dateErr := time.Parse(time.RFC3339Nano, b.Created)
	if !exists || b.ProfileVersion != profile.Version || b.Seed < 1 || b.Seed > 999999 || len(b.SourceRevision) != 64 || dateErr != nil {
		return b, fmt.Errorf("baseline: metadata profile/version/seed/source/date повреждены")
	}
	return b, nil
}
func (c *Checker) files(p Profile, workspace string) {
	read := func(rel string) string {
		b, e := probeReadFile(filepath.Join(workspace, rel))
		if e != nil {
			return ""
		}
		return string(b)
	}
	switch p.Kind {
	case "files":
		wantOriginal, wantLog, wanted, _, _ := fixtureTexts(c.r.VariantSeed)
		m, e := readInstance(workspace, c.instance)
		c.add("fixture.seed", e == nil && m.Seed == c.r.VariantSeed && wantOriginal != "", c.r.VariantSeed, m.Seed, "Используйте seed назначенной попытки, а не другую исходную задачу")
		original := read("inputs/отчёт смены.txt")
		copy := read("evidence/отчёт смены.txt")
		c.add("file.original", original == wantOriginal && wantOriginal != "", wantOriginal, original, "Оригинал остаётся в inputs")
		c.add("log.original", read("inputs/service.log") == wantLog && wantLog != "", wantLog, read("inputs/service.log"), "Исходный журнал нельзя изменять ради совпадения результата")
		c.add("file.copy", copy == original && copy != "", "точная копия оригинала", copy, "Кавычки сохраняют путь с пробелом одним аргументом")
		got := read(fmt.Sprintf("evidence/T%d.log", c.r.VariantSeed))
		c.add("log.selection", got == wanted && wanted != "", wanted, got, "Выберите все строки назначенного ticket ID и исключите чужие заявки")
	case "linux":
		permissions, e := probeFileMode(filepath.Join(workspace, "inputs/only-owner.txt"))
		mode := "missing"
		ok := false
		if e == nil {
			mode = fmt.Sprintf("%04o", permissions)
			ok = permissions == 0600
		}
		c.add("file.permissions", ok, "0600: owner read/write, без доступа группе и остальным", mode, "Добавьте чтение владельцу, не расширяя остальные права")
	case "terminal":
		_, _, _, _, unique := fixtureTexts(c.r.VariantSeed)
		c.add("stdout.content", read("evidence/stdout.txt") == "profile intentional-failure\n", "profile intentional-failure", read("evidence/stdout.txt"), "Сохраните stdout отдельно")
		c.add("stderr.content", read("evidence/stderr.txt") == "FAIL fixture.invalid\n", "FAIL fixture.invalid", read("evidence/stderr.txt"), "2> относится к stderr")
		c.add("log.unique", read("evidence/codes.txt") == unique && unique != "", unique, read("evidence/codes.txt"), "Получите уникальные строки назначенного codes.log")
	case "git":
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		out, e := probeCommand(ctx, "/usr/bin/git", "-C", filepath.Join(workspace, "repo"), "ls-tree", "--name-only", "HEAD").Output()
		c.add("git.tree", e == nil && string(out) == "README.txt\n", "HEAD содержит только README.txt", string(out), "Посмотрите индекс перед commit, не добавляйте черновик")
		out, e = probeCommand(ctx, "/usr/bin/git", "-C", filepath.Join(workspace, "repo"), "show", "HEAD:README.txt").Output()
		c.add("git.config", e == nil && string(out) == "TicketLab\nport=18005\n", "README.txt содержит TicketLab и port=18005", string(out), "Создание ветки само по себе не меняет конфигурацию")
		out, e = probeCommand(ctx, "/usr/bin/git", "-C", filepath.Join(workspace, "repo"), "status", "--porcelain", "--untracked-files=all").Output()
		c.add("git.draft", e == nil && strings.Contains(string(out), "?? draft.txt"), "draft.txt остаётся untracked", string(out), "Черновик не должен попасть в commit")
	}
}
func checkMain(args []string) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("labcheck version | doctor --workspace PATH --module N | check PROFILE --instance ID --workspace PATH --base-url URL --report PATH --state BASELINE | baseline PROFILE --instance ID --base-url URL --state PATH")
		return 0
	}
	if args[0] == "version" {
		fmt.Println("labcheck 3.0.0 profile schema 1.0")
		return 0
	}
	if args[0] == "intentional-failure" {
		fmt.Println("profile intentional-failure")
		fmt.Fprintln(os.Stderr, "FAIL fixture.invalid")
		return 1
	}
	if args[0] == "doctor" {
		f := flag.NewFlagSet("doctor", flag.ContinueOnError)
		w := f.String("workspace", "", "workspace")
		m := f.Int("module", 1, "модуль")
		if f.Parse(args[1:]) != nil {
			return 64
		}
		fmt.Printf("OS=%s arch=%s workspace=%s\n", runtime.GOOS, runtime.GOARCH, *w)
		code := 0
		if runtime.GOOS != "linux" {
			fmt.Println("НЕ ПРОВЕРЕНО Ubuntu: откройте Ubuntu, не PowerShell")
			code = 2
		}
		tools := []string{"bash", "grep", "cp", "ps", "ss"}
		if *m >= 3 {
			tools = append(tools, "curl")
		}
		if *m >= 5 {
			tools = append(tools, "git")
		}
		if *m >= 7 {
			tools = append(tools, "docker")
		}
		for _, t := range tools {
			_, e := exec.LookPath(t)
			if e != nil {
				fmt.Println("НЕ ПРОВЕРЕНО инструмент", t)
				code = 2
			} else {
				fmt.Println("PASS инструмент", t)
			}
		}
		if *w != "" {
			e := os.MkdirAll(*w, 0700)
			if e != nil {
				fmt.Println("workspace недоступен")
				code = 2
			}
		}
		if doctorRuntime(*w, *m) != 0 {
			code = 2
		}
		return code
	}
	if (args[0] != "check" && args[0] != "baseline") || len(args) < 2 {
		return 64
	}
	var profiles map[string]Profile
	json.Unmarshal(profileBytes, &profiles)
	p, ok := profiles[args[1]]
	if !ok {
		fmt.Fprintln(os.Stderr, "неизвестный профиль")
		return 64
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	instance := f.String("instance", "", "instance")
	base := f.String("base-url", "", "loopback URL")
	workspace := f.String("workspace", "", "workspace")
	report := f.String("report", "", "JSON report path")
	state := f.String("state", "", "immutable baseline path")
	rel := f.String("expected-release", "3.0.0-good", "из известной поставки")
	source := f.String("expected-source", sourceRevision, "из known manifest")
	seed := f.Int("seed", 417, "variant seed")
	container := f.String("container", "", "назначенный контейнер API с labels")
	image := f.String("expected-image", "", "image ID известной поставки")
	postgres := f.String("postgres-container", "", "назначенный контейнер PostgreSQL")
	database := f.String("database", "ticketlab", "учебная БД")
	restoreDB := f.String("restore-database", "", "другая БД для SQL restore probe")
	peerWorkspace := f.String("peer-workspace", "", "workspace второго сохраняемого instance")
	peerInstance := f.String("peer-instance", "", "ID второго instance")
	peerPG := f.String("peer-postgres-container", "", "назначенный PostgreSQL другого стенда")
	peerDB := f.String("peer-database", "", "переданная БД на другом стенде")
	peerURL := f.String("peer-base-url", "", "loopback URL другого стенда")
	if f.Parse(args[2:]) != nil || *instance == "" {
		return 64
	}
	if *base != "" {
		if !safeBaseURL(*base) {
			fmt.Fprintln(os.Stderr, "target должен быть назначенным loopback HTTP URL без redirect/auth/path")
			return 64
		}
	}
	lockDir := filepath.Join(os.TempDir(), "peaky301758-check-locks")
	if e := os.MkdirAll(lockDir, 0700); e != nil {
		return 2
	}
	key := sha256.Sum256([]byte(*instance + "\x00" + *base + "\x00" + *workspace))
	lock, e := storeLock(filepath.Join(lockDir, fmt.Sprintf("%x.lock", key)))
	if e != nil {
		fmt.Fprintln(os.Stderr, "instance занят другой проверкой")
		return 2
	}
	defer lock.Close()
	r := Report{"1.0", "3.0.0", args[1], p.Version, *instance, marker(), *seed, "local_self_check", now(), "", []Check{}, "running", 2, []string{"Локальный отчёт не подтверждает авторство и не начисляет баллы Stepik"}}
	if _, remote := probeSSH(); remote {
		r.Scope = "server_observed_author_test"
		r.Limits = []string{"Сервер сам наблюдает назначенный учебный стенд. Попытка пока не привязана к пользователю Stepik; баллы не начисляются. Docker root внутри VM не является защищённой экзаменационной средой."}
	}
	c := Checker{*base, *instance, *rel, *source, &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect запрещён") }}, &r}
	if args[0] == "baseline" {
		if *base == "" || *state == "" {
			return 64
		}
		if _, e := os.Stat(*state); e == nil {
			fmt.Fprintln(os.Stderr, "baseline уже существует; не перезаписываем")
			return 64
		}
		var version map[string]string
		vs, ve := c.json("GET", "/version", nil, "", &version)
		if ve != nil || vs != 200 || version["instance_id"] != *instance || version["source_revision"] != *source || version["release_version"] != *rel {
			fmt.Fprintln(os.Stderr, "baseline не создан: instance и известная версия до операции не подтверждены")
			return 1
		}
		var t Ticket
		s, e := c.json("POST", "/tickets", map[string]string{"title": "baseline-" + marker()}, "baseline-"+marker(), &t)
		if e != nil || s != 201 || t.ID == "" {
			fmt.Fprintln(os.Stderr, "baseline не создан: операция POST не подтверждена")
			return 1
		}
		var list struct {
			Tickets []Ticket `json:"items"`
			Total   int      `json:"total"`
		}
		ls, le := c.json("GET", "/tickets?limit=100", nil, "", &list)
		if le != nil || ls != 200 || list.Total != len(list.Tickets) || len(list.Tickets) == 0 {
			fmt.Fprintln(os.Stderr, "baseline не создан: не удалось получить полный набор записей (лимит учебной попытки 100)")
			return 2
		}
		b := Baseline{SchemaVersion: "1.0", Profile: args[1], Instance: *instance, Created: now(), Tickets: list.Tickets, ProfileVersion: p.Version, SourceRevision: *source, Seed: *seed}
		if *container != "" {
			probe, e := inspectOwned(*container, *instance)
			if e != nil {
				fmt.Fprintln(os.Stderr, e)
				return 2
			}
			b.ContainerID = probe.ID
			b.Volume = volumeReference(probe, "/data")
		}
		if *postgres != "" {
			pg, e := pgSnapshot(*postgres, *instance, *database)
			if e != nil {
				fmt.Fprintln(os.Stderr, e)
				return 2
			}
			b.Postgres = &pg
		}
		if e = atomicJSON(*state, b, true); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		fmt.Println("baseline сохранён", *state)
		return 0
	}
	if p.Kind == "http" {
		if *base == "" {
			return 64
		}
		c.httpChecks(p, *state)
		c.runtimeProbes(p, *state, *container, *image, *postgres, *database)
		c.ciProbes(p, *workspace)
		c.restoreProbes(p, *state, *postgres, *database, *restoreDB)
		c.cleanRoomProbes(p, *state, *postgres, *database, *peerPG, *peerInstance, *peerDB, *peerURL)
	} else {
		if *workspace == "" {
			return 64
		}
		c.files(p, *workspace)
		c.earlyRuntime(p, *workspace, *peerWorkspace, *peerInstance)
	}
	for _, id := range p.Mandatory {
		found := false
		for _, ch := range r.Checks {
			if ch.ID == id {
				found = true
			}
		}
		if !found {
			r.Checks = append(r.Checks, Check{id, "not_checked", "достаточный runtime probe", "probe не предоставлен", "Нельзя подтвердить это свойство одним HTTP; нужен отдельный probe"})
		}
	}
	code := 0
	for _, ch := range r.Checks {
		if ch.Status == "fail" && code == 0 {
			code = 1
		}
		if ch.Status == "not_checked" {
			code = 2
		}
		fmt.Printf("%s %s\n  ожидалось: %v\n  получено: %v\n", strings.ToUpper(ch.Status), ch.ID, ch.Expected, ch.Actual)
		if ch.Status != "pass" {
			fmt.Println("  следующая проверка:", ch.Hint)
		}
	}
	r.ExitCode = code
	r.Ended = now()
	r.Overall = "passed"
	if code == 1 {
		r.Overall = "failed"
	}
	if code == 2 {
		r.Overall = "not_evaluated"
	}
	if *report != "" {
		if e := atomicJSON(*report, r, false); e != nil {
			fmt.Fprintln(os.Stderr, "отчёт не сохранён:", e)
			return 2
		}
	}
	return code
}

func safeBaseURL(base string) bool {
	u, e := url.Parse(base)
	return e == nil && u.Scheme == "http" && u.User == nil && u.RawQuery == "" && u.Path == "" && u.Fragment == "" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

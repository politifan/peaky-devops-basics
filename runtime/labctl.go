package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Instance struct {
	CourseID       int    `json:"course_id"`
	ModuleID       string `json:"module_id"`
	ProfileVersion string `json:"profile_version"`
	InstanceID     string `json:"instance_id"`
	Seed           int    `json:"seed"`
	Port           int    `json:"port"`
	PID            int    `json:"pid"`
	Ownership      string `json:"ownership"`
}

func ownedPath(root, id string) (string, error) {
	if !filepath.IsAbs(root) || !regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`).MatchString(id) {
		return "", fmt.Errorf("root должен быть абсолютным, instance — безопасным именем")
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return "", e
	}
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	p := filepath.Join(canonical, id)
	if st, e := os.Lstat(p); e == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("instance не может быть symlink")
		}
		resolved, e := filepath.EvalSymlinks(p)
		if e != nil || filepath.Dir(resolved) != canonical {
			return "", fmt.Errorf("instance вышел за root")
		}
	}
	return p, nil
}
func readInstance(p, id string) (Instance, error) {
	var m Instance
	b, e := probeReadFile(filepath.Join(p, ".peaky-instance.json"))
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal(b, &m); e != nil || m.CourseID != 301758 || m.InstanceID != id || m.Ownership != "peaky301758-v3" {
		return m, fmt.Errorf("ownership marker не соответствует instance")
	}
	return m, nil
}
func ctlMain(args []string) int {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("labctl prepare mNN --instance ID --seed N --root ABSOLUTE | start/status/stop --instance ID --root ABSOLUTE | reset --instance ID --confirm-instance ID --root ABSOLUTE")
		return 0
	}
	command := args[0]
	module := ""
	offset := 1
	if command == "prepare" {
		if len(args) < 2 {
			return 64
		}
		module = args[1]
		offset = 2
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	id := f.String("instance", "", "instance")
	root := f.String("root", "", "absolute labs root")
	seed := f.Int("seed", 417, "fixture variant")
	confirm := f.String("confirm-instance", "", "точное подтверждение reset")
	port := f.Int("port", 0, "назначенный свободный порт")
	backupFrom := f.String("backup-from", "", "путь дампа, переданного для собственной новой попытки")
	releaseChoice := f.String("release", "good", "готовая поставка good/bad-body/bad-write/bad-retry")
	if f.Parse(args[offset:]) != nil {
		return 64
	}
	p, e := ownedPath(*root, *id)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 64
	}
	if command == "prepare" {
		n, e := strconv.Atoi(strings.TrimPrefix(module, "m"))
		if e != nil || n < 1 || n > 12 || *seed < 1 || *seed > 999999 {
			return 64
		}
		if _, e = os.Stat(p); e == nil {
			fmt.Fprintln(os.Stderr, "instance уже существует; prepare не перезаписывает данные")
			return 64
		}
		if e = os.Mkdir(p, 0700); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		if *port == 0 {
			*port = 18000 + n
		}
		m := Instance{301758, module, "1.0", *id, *seed, *port, 0, "peaky301758-v3"}
		if e = atomicJSON(filepath.Join(p, ".peaky-instance.json"), m, true); e != nil {
			return 2
		}
		for _, dir := range []string{"inputs", "evidence", "data", "config", "state", "reports"} {
			os.Mkdir(filepath.Join(p, dir), 0700)
		}
		write := func(rel, text string) {
			if e := os.WriteFile(filepath.Join(p, rel), []byte(text), 0600); e != nil {
				panic(e)
			}
		}
		original, log, _, codes, _ := fixtureTexts(*seed)
		write("inputs/отчёт смены.txt", original)
		write("inputs/service.log", log)
		write("inputs/codes.log", codes)
		write("inputs/only-owner.txt", "учебный файл\n")
		os.Chmod(filepath.Join(p, "inputs/only-owner.txt"), 0200)
		write("config/service.env", "TICKETLAB_DATA_DIR="+filepath.Join(p, "data")+"\n")
		write("config/pipeline.env", "pipefail=off\n")
		if n == 5 {
			repo := filepath.Join(p, "repo")
			os.Mkdir(repo, 0700)
			write("repo/README.txt", "TicketLab\nport=18004\n")
			write("repo/draft.txt", "локальный черновик\n")
			for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Учебный автор"}, {"config", "user.email", "student@example.invalid"}, {"add", "README.txt"}, {"commit", "-m", "Начальная конфигурация"}} {
				all := append([]string{"-C", repo}, a...)
				if b, e := exec.Command("git", all...).CombinedOutput(); e != nil {
					fmt.Println(string(b))
					return 2
				}
			}
		}
		if n >= 7 {
			if e = prepareDockerLab(p, m); e != nil {
				fmt.Fprintln(os.Stderr, e)
				return 2
			}
		}
		fmt.Println("prepare:", p, "seed", m.Seed, "port", m.Port)
		return 0
	}
	m, e := readInstance(p, *id)
	if e != nil {
		fmt.Fprintln(os.Stderr, "не найден назначенный instance:", e)
		return 64
	}
	if command == "backup" || command == "restore" {
		return backupControl(command, p, m)
	}
	if command == "select-release" {
		return selectControl(p, m, *releaseChoice)
	}
	if command == "import-backup" {
		if m.ModuleID != "m11" || !filepath.IsAbs(*backupFrom) {
			return 64
		}
		f, e := os.Open(*backupFrom)
		if e != nil {
			return 2
		}
		defer f.Close()
		if e = copyStream(filepath.Join(p, "state/backup.dump"), f); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		copied, e := os.ReadFile(filepath.Join(p, "state/backup.dump"))
		if e != nil || len(copied) < 5 || string(copied[:5]) != "PGDMP" {
			fmt.Fprintln(os.Stderr, "переданный файл не является custom pg_dump; restore недоступен")
			return 1
		}
		hash := sha256.Sum256(copied)
		if e = atomicJSON(filepath.Join(p, "state/backup.json"), map[string]any{"course_id": 301758, "instance_id": m.InstanceID, "created_utc": now(), "dump_sha256": hex.EncodeToString(hash[:]), "scope": "imported dump; SQL and HTTP acceptance required"}, true); e != nil {
			return 2
		}
		fmt.Println("дамп передан в новую собственную попытку; существующие данные не заменены")
		return 0
	}
	if dockerLab(m) && (command == "start" || command == "stop" || command == "status") {
		return dockerControl(command, p, m)
	}
	switch command {
	case "status":
		fmt.Printf("instance=%s module=%s port=%d pid=%d root=%s\n", m.InstanceID, m.ModuleID, m.Port, m.PID, p)
		if m.PID != 0 && !instanceProcess(m.PID, m.InstanceID) {
			fmt.Println("процесс не подтверждён")
			return 1
		}
		return 0
	case "start":
		if m.PID != 0 && instanceProcess(m.PID, m.InstanceID) {
			fmt.Println("уже запущен собственный instance")
			return 0
		}
		listener, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", m.Port))
		if e != nil {
			fmt.Fprintln(os.Stderr, "порт занят; чужой процесс не останавливаем")
			return 2
		}
		listener.Close()
		configuration, e := os.ReadFile(filepath.Join(p, "config/service.env"))
		if e != nil {
			fmt.Fprintln(os.Stderr, "config/service.env недоступен")
			return 2
		}
		setting := strings.TrimSpace(string(configuration))
		if !strings.HasPrefix(setting, "TICKETLAB_DATA_DIR=") || strings.Contains(setting, "\n") {
			fmt.Fprintln(os.Stderr, "ожидается единственный параметр TICKETLAB_DATA_DIR")
			return 64
		}
		dataDir := strings.TrimPrefix(setting, "TICKETLAB_DATA_DIR=")
		if !filepath.IsAbs(dataDir) {
			return 64
		}
		dataDir = filepath.Clean(dataDir)
		relPath, e := filepath.Rel(p, dataDir)
		if e != nil || relPath == "." || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			fmt.Fprintln(os.Stderr, "data-dir должен оставаться внутри собственного instance")
			return 64
		}
		cursor := p
		for _, component := range strings.Split(relPath, string(filepath.Separator)) {
			cursor = filepath.Join(cursor, component)
			st, se := os.Lstat(cursor)
			if se == nil && st.Mode()&os.ModeSymlink != 0 {
				fmt.Fprintln(os.Stderr, "symlink в data-dir отвергнут до создания папок")
				return 64
			}
			if se != nil && !os.IsNotExist(se) {
				return 2
			}
		}
		if e = os.MkdirAll(dataDir, 0700); e != nil {
			return 2
		}
		resolved, e := filepath.EvalSymlinks(dataDir)
		if e != nil || resolved != dataDir {
			fmt.Fprintln(os.Stderr, "data-dir с symlink отвергнут")
			return 64
		}
		own, _ := os.Executable()
		app := filepath.Join(filepath.Dir(own), "ticketlab")
		if selection, se := os.ReadFile(filepath.Join(p, "config/release.env")); se == nil {
			value := strings.TrimSpace(string(selection))
			if !strings.HasPrefix(value, "release=") {
				return 64
			}
			app, e = selectedBinary(strings.TrimPrefix(value, "release="))
			if e != nil {
				return 64
			}
		}
		log, e := os.OpenFile(filepath.Join(p, "evidence/service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return 2
		}
		defer log.Close()
		cmd := exec.Command(app, "serve", "--listen", fmt.Sprintf("127.0.0.1:%d", m.Port), "--data-dir", dataDir, "--instance", m.InstanceID)
		cmd.Stdout = log
		cmd.Stderr = log
		cmd.Stdin = nil
		detach(cmd)
		if e = cmd.Start(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		m.PID = cmd.Process.Pid
		cmd.Process.Release()
		if e = atomicJSON(filepath.Join(p, ".peaky-instance.json"), m, false); e != nil {
			return 2
		}
		fmt.Println("start pid", m.PID)
		client := &http.Client{Timeout: time.Second}
		for attempt := 0; attempt < 30; attempt++ {
			response, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d/version", m.Port))
			if e == nil {
				var v map[string]string
				e = json.NewDecoder(response.Body).Decode(&v)
				response.Body.Close()
				if e == nil && response.StatusCode == 200 && v["instance_id"] == m.InstanceID {
					return 0
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Fprintln(os.Stderr, "назначенный instance не подтвердил запуск; прочитайте evidence/service.log")
		return 2
	case "stop":
		if m.PID != 0 {
			if !instanceProcess(m.PID, m.InstanceID) {
				fmt.Fprintln(os.Stderr, "PID не соответствует instance; сигнал не отправлен")
				return 2
			}
			if e = stopProcess(m.PID); e != nil {
				fmt.Fprintln(os.Stderr, e)
				return 2
			}
			for attempt := 0; attempt < 50 && instanceProcess(m.PID, m.InstanceID); attempt++ {
				time.Sleep(100 * time.Millisecond)
			}
			if instanceProcess(m.PID, m.InstanceID) {
				fmt.Fprintln(os.Stderr, "штатное завершение не подтверждено; PID сохранён")
				return 2
			}
		}
		m.PID = 0
		atomicJSON(filepath.Join(p, ".peaky-instance.json"), m, false)
		return 0
	case "reset":
		fmt.Println("scope reset:", p, "evidence/state сохраняются")
		if *confirm != m.InstanceID {
			fmt.Fprintln(os.Stderr, "нужно точное --confirm-instance")
			return 64
		}
		if dockerLab(m) {
			if e = dockerReset(p, m); e != nil {
				fmt.Fprintln(os.Stderr, e)
				return 64
			}
			fmt.Println("собственные контейнеры удалены; volumes, конфигурация, baseline и данные сохранены; для чистой попытки новый instance")
			return 0
		}
		if m.PID != 0 && instanceProcess(m.PID, m.InstanceID) {
			fmt.Fprintln(os.Stderr, "сначала штатно stop собственного instance")
			return 64
		}
		for _, d := range []string{"inputs", "data", "config", "repo"} {
			target := filepath.Join(p, d)
			st, e := os.Lstat(target)
			if e == nil && st.Mode()&os.ModeSymlink != 0 {
				fmt.Fprintln(os.Stderr, "reset отверг symlink:", d)
				return 64
			}
			if e = os.RemoveAll(target); e != nil {
				return 2
			}
		}
		fmt.Println("runtime очищен; baseline/evidence сохранены. Для новой попытки используйте новый instance ID")
		return 0
	default:
		return 64
	}
}

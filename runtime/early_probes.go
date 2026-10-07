package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (c *Checker) earlyRuntime(p Profile, workspace, peerWorkspace, peerInstance string) {
	if p.Kind == "linux" {
		m, e := readInstance(workspace, c.instance)
		pass := e == nil && m.PID != 0 && instanceProcess(m.PID, c.instance)
		pid := 0
		if e == nil {
			pid = m.PID
		}
		c.add("process.identity", pass, "процесс именно назначенного instance запущен", map[string]any{"pid": pid, "identity_matches": pass}, "Сверьте state PID и аргументы процесса; не используйте глобальный pkill")
		if peerWorkspace != "" && peerInstance != "" {
			other, e := readInstance(peerWorkspace, peerInstance)
			ok := e == nil && other.PID != 0 && instanceProcess(other.PID, peerInstance)
			c.add("process.peer", ok, "второй назначенный процесс сохранён", map[string]any{"instance": peerInstance, "pid": other.PID, "running": ok}, "Исправляйте один instance; второй должен остаться работающим")
		}
	}
	if p.Kind == "terminal" {
		setting, e := probeReadFile(filepath.Join(workspace, "config/pipeline.env"))
		if e != nil {
			c.r.Checks = append(c.r.Checks, Check{"pipeline.exit", "not_checked", "явная настройка pipefail", "config/pipeline.env недоступен", "Нужен fixture терминальной цепочки"})
			return
		}
		mode := strings.TrimSpace(string(setting))
		if mode != "pipefail=on" && mode != "pipefail=off" {
			c.add("pipeline.exit", false, "pipefail=on/off", mode, "Допустим только объявленный параметр, не произвольный shell-код")
			return
		}
		script := `"$1" intentional-failure | tee "$2"`
		if mode == "pipefail=on" {
			script = "set -o pipefail; " + script
		}
		exe, _ := os.Executable()
		exe = filepath.Join(filepath.Dir(exe), "labcheck")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, remote := probeSSH(); remote {
			script = `(printf 'profile intentional-failure\n'; printf 'FAIL fixture.invalid\n' >&2; exit 1) | tee "$1"`
			if mode == "pipefail=on" {
				script = "set -o pipefail; " + script
			}
		}
		var cmd *exec.Cmd
		if _, remote := probeSSH(); remote {
			cmd = probeCommand(ctx, "/bin/bash", "--noprofile", "--norc", "-c", script, "peaky-fixed-probe", filepath.Join(workspace, "evidence/grader-pipeline.log"))
		} else {
			cmd = probeCommand(ctx, "bash", "--noprofile", "--norc", "-c", script, "peaky-fixed-probe", exe, filepath.Join(workspace, "evidence/grader-pipeline.log"))
		}
		out, e := cmd.CombinedOutput()
		code := 0
		if e != nil {
			if exit, ok := e.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				c.r.Checks = append(c.r.Checks, Check{"pipeline.exit", "not_checked", "реальный запуск фиксированной цепочки", errorText(e), "Проверьте наличие Bash и доступ к учебному evidence"})
				return
			}
		}
		c.add("pipeline.exit", code == 1, "падающий checker сохраняет exit code 1 через tee", map[string]any{"actual_exit_code": code, "mode": mode, "output": fmt.Sprintf("%.300s", out)}, "Успех tee может скрыть отказ предыдущей стадии; проверьте pipefail")
	}
}

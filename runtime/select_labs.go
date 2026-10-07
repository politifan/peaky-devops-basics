package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func selectedBinary(release string) (string, error) {
	file := "ticketlab"
	if release != "good" {
		if release != "bad-write" && release != "bad-body" && release != "bad-retry" {
			return "", fmt.Errorf("неизвестная готовая поставка")
		}
		file += "-" + release
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), file), nil
}
func selectControl(p string, m Instance, release string) int {
	binary, e := selectedBinary(release)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 64
	}
	if !dockerLab(m) {
		if m.PID != 0 && instanceProcess(m.PID, m.InstanceID) {
			fmt.Fprintln(os.Stderr, "сначала stop собственного экземпляра")
			return 64
		}
		if e = os.WriteFile(filepath.Join(p, "config/release.env"), []byte("release="+release+"\n"), 0600); e != nil {
			return 2
		}
		fmt.Println("выбрана готовая поставка", release, "для следующего start")
		return 0
	}
	name := "peaky301758-" + m.InstanceID + "-api-1"
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	// A present container must be ours, even when stopped. Inspection errors
	// must never authorize mutation of a foreign or inaccessible container.
	listed, listErr := exec.CommandContext(ctx, "docker", "container", "ls", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}").Output()
	if listErr != nil {
		fmt.Fprintln(os.Stderr, "не удалось проверить владельца контейнера")
		return 2
	}
	if len(listed) != 0 {
		state, err := inspectOwned(name, m.InstanceID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "контейнер с этим именем не подтверждён как свой")
			return 64
		}
		if state.State.Running {
			fmt.Fprintln(os.Stderr, "сначала stop своего api")
			return 64
		}
	}
	// Preserve the verified rollback image before replacing any local binary.
	data, readErr := os.ReadFile(filepath.Join(p, "state/known-image.json"))
	var retained map[string]string
	if readErr == nil {
		if json.Unmarshal(data, &retained) != nil || retained["image_id"] == "" {
			fmt.Fprintln(os.Stderr, "неверная запись исходного образа")
			return 2
		}
		if e = exec.CommandContext(ctx, "docker", "image", "tag", retained["image_id"], "peaky301758-"+m.InstanceID+":known-good").Run(); e != nil {
			fmt.Fprintln(os.Stderr, "исходный проверенный образ недоступен; смена поставки отменена")
			return 2
		}
	} else if !os.IsNotExist(readErr) {
		return 2
	}
	bytes, e := os.ReadFile(binary)
	if e != nil {
		return 2
	}
	if e = os.WriteFile(filepath.Join(p, "image/ticketlab"), bytes, 0755); e != nil {
		return 2
	}
	if release == "good" {
		// Restore the originally verified immutable image, avoiding changing provenance during an unnecessary rebuild.
		data, e := os.ReadFile(filepath.Join(p, "state/known-image.json"))
		var known map[string]string
		if e == nil && json.Unmarshal(data, &known) == nil && known["image_id"] != "" {
			cmd := exec.CommandContext(ctx, "docker", "image", "tag", known["image_id"], "peaky301758-"+m.InstanceID+":3.0.0")
			if e = cmd.Run(); e != nil {
				fmt.Fprintln(os.Stderr, "исходный проверенный образ недоступен; новый не подставляется вместо него")
				return 2
			}
			fmt.Println("возвращён исходный проверенный image ID")
			return 0
		}
	}
	if e = composeRun(p, m, "build", "api"); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	fmt.Println("подготовлена готовая поставка", release, "start применит её к своему api")
	return 0
}

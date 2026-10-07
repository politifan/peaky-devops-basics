package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ContainerProbe struct {
	ID    string `json:"Id"`
	Image string `json:"Image"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

func inspectOwned(name, instance string) (ContainerProbe, error) {
	var out []ContainerProbe
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	b, e := probeCommand(ctx, "/usr/bin/docker", "inspect", "--type", "container", name).Output()
	if e != nil {
		return ContainerProbe{}, fmt.Errorf("Docker CLI/daemon/доступ или назначенный контейнер недоступны")
	}
	if json.Unmarshal(b, &out) != nil || len(out) != 1 {
		return ContainerProbe{}, fmt.Errorf("некорректный inspect")
	}
	p := out[0]
	if p.Config.Labels["io.peaky.course"] != "301758" || p.Config.Labels["io.peaky.instance"] != instance {
		return ContainerProbe{}, fmt.Errorf("контейнер не принадлежит назначенному instance")
	}
	return p, nil
}
func volumeReference(p ContainerProbe, destination string) string {
	for _, m := range p.Mounts {
		if m.Destination == destination {
			if m.Type == "volume" {
				return "volume:" + m.Name
			}
			if m.Type == "bind" {
				return "bind:" + m.Source
			}
		}
	}
	return ""
}
func (c *Checker) runtimeProbes(p Profile, state, container, image, postgres, db string) {
	var baseline Baseline
	if state != "" {
		baseline, _ = loadBaseline(state, c.instance)
	}
	if container != "" {
		got, e := inspectOwned(container, c.instance)
		if e != nil {
			for _, id := range []string{"container.recreate", "container.image", "container.volume"} {
				if has(p, id) {
					c.r.Checks = append(c.r.Checks, Check{id, "not_checked", "назначенный runtime inspect", errorText(e), "Проверьте Docker daemon, доступ и labels собственного instance"})
				}
			}
		} else {
			if has(p, "container.recreate") {
				if baseline.ContainerID == "" {
					c.r.Checks = append(c.r.Checks, Check{"container.recreate", "not_checked", "container ID до операции", "baseline не содержит runtime identity", "Сохраните baseline с --container до recreate"})
				} else {
					c.add("container.recreate", got.ID != baseline.ContainerID && got.State.Running, "новый ID после recreate; running", map[string]any{"old": baseline.ContainerID, "new": got.ID, "running": got.State.Running}, "restart сохраняет ID; для этого профиля нужен recreate собственного контейнера")
				}
			}
			if has(p, "container.image") {
				if image == "" {
					c.r.Checks = append(c.r.Checks, Check{"container.image", "not_checked", "known image ID из manifest", "ожидание не задано", "Передайте image ID из известной поставки"})
				} else {
					c.add("container.image", got.Image == image, image, got.Image, "Tag не доказывает, какой image запущен; сверяйте inspect Image")
				}
			}
			if has(p, "container.volume") && !has(p, "postgres.identity") {
				vol := volumeReference(got, "/data")
				if baseline.Volume == "" {
					c.r.Checks = append(c.r.Checks, Check{"container.volume", "not_checked", "исходный mount /data", "runtime baseline отсутствует", "Новый пустой volume не заменяет старые данные"})
				} else {
					c.add("container.volume", vol == baseline.Volume && vol != "", baseline.Volume, vol, "Проверьте источник mount и data-dir, не удаляйте прежний volume")
				}
			}
		}
	}
	if postgres != "" && has(p, "postgres.identity") {
		pg, e := inspectOwned(postgres, c.instance)
		if e != nil {
			c.r.Checks = append(c.r.Checks, Check{"postgres.identity", "not_checked", "назначенный PostgreSQL runtime", errorText(e), "Проверьте labels и доступ к учебному db"})
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			out, e := probeCommand(ctx, "/usr/bin/docker", "exec", postgres, "psql", "-U", "student", "-d", db, "-Atc", "SELECT version(); SELECT revision FROM ticketlab_schema WHERE singleton=true;").Output()
			text := string(out)
			c.add("postgres.identity", e == nil && strings.HasPrefix(text, "PostgreSQL 16"), "реальный SELECT version() PostgreSQL 16", strings.Split(text, "\n")[0], "HTTP /version не доказывает фактический backend; нужен SQL probe")
			if has(p, "postgres.schema") {
				lines := strings.Split(strings.TrimSpace(text), "\n")
				actual := ""
				if len(lines) > 1 {
					actual = lines[1]
				}
				c.add("postgres.schema", e == nil && actual == "1", "schema revision 1", actual, "pg_isready не проверяет прикладную схему; выполните готовую migrate --check")
			}
			if has(p, "container.volume") {
				ref := volumeReference(pg, "/var/lib/postgresql/data")
				c.add("container.volume", ref != "", "явный mount в PGDATA /var/lib/postgresql/data", ref, "Volume должен монтироваться в фактический PGDATA выбранного образа")
			}
		}
	}
}

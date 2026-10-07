package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const ubuntuImage = "ubuntu@sha256:534baea6a22c03a63003dbc8dbe78fe34bc0d7e595d9a9dc9834884ff530eb55"
const postgresImage = "postgres@sha256:1a6ab3f5345eb6dbe04a1349529caabdb0ab09293a09590fad07b2246bfa4b54"

func dockerLab(m Instance) bool { return m.ModuleID >= "m07" }

func prepareDockerLab(p string, m Instance) error {
	osRelease, e := os.ReadFile("/etc/os-release")
	if e != nil || !strings.Contains(string(osRelease), `ID=ubuntu`) || !strings.Contains(string(osRelease), `VERSION_ID="24.04"`) {
		return fmt.Errorf("Docker-лаборатории готовятся внутри собственной Ubuntu 24.04 VM")
	}
	exe, _ := os.Executable()
	bin, e := os.ReadFile(filepath.Join(filepath.Dir(exe), "ticketlab"))
	if e != nil {
		return e
	}
	contextDir := filepath.Join(p, "image")
	if e = os.Mkdir(contextDir, 0700); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(contextDir, "ticketlab"), bin, 0755); e != nil {
		return e
	}
	dockerfile := "FROM " + ubuntuImage + "\nCOPY --chmod=0755 ticketlab /usr/local/bin/ticketlab\nUSER 10001\nENTRYPOINT [\"/usr/local/bin/ticketlab\"]\n"
	if e = os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte(dockerfile), 0600); e != nil {
		return e
	}
	imageName := "peaky301758-" + m.InstanceID + ":3.0.0"
	env := fmt.Sprintf("TICKETLAB_IMAGE=%s\nTICKETLAB_PORT=%d\nTICKETLAB_DATA_DIR=/data\nSTUDENT_UID=%d\nSTUDENT_GID=%d\n", imageName, m.Port, os.Getuid(), os.Getgid())
	labels := fmt.Sprintf("    labels:\n      io.peaky.course: '301758'\n      io.peaky.instance: '%s'\n", m.InstanceID)
	compose := "services:\n  api:\n    image: ${TICKETLAB_IMAGE}\n    build: ./image\n    pull_policy: never\n    user: '${STUDENT_UID}:${STUDENT_GID}'\n    mem_limit: 96m\n    cpus: 0.25\n    pids_limit: 64\n    restart: 'no'\n    ports:\n      - '127.0.0.1:${TICKETLAB_PORT}:18000'\n" + labels
	if m.ModuleID == "m07" || m.ModuleID == "m09" || m.ModuleID == "m12" {
		compose += "    command: ['serve', '--listen', '0.0.0.0:18000', '--instance', '" + m.InstanceID + "', '--data-dir', '${TICKETLAB_DATA_DIR}']\n    volumes:\n      - './data:/data'\n"
	} else {
		admin, key := marker(), marker()
		env += "POSTGRES_PASSWORD=" + admin + "\nSTUDENT_PASSWORD=" + key + "\nTICKETLAB_DATABASE_URL=postgres://student:" + key + "@db:5432/ticketlab?sslmode=disable\n"
		compose += "    command: ['serve', '--listen', '0.0.0.0:18000', '--instance', '" + m.InstanceID + "', '--data-dir', '/data', '--storage', 'postgresql']\n    environment:\n      TICKETLAB_DATABASE_URL: ${TICKETLAB_DATABASE_URL}\n    depends_on:\n      db:\n        condition: service_healthy\n  db:\n    image: " + postgresImage + "\n    pull_policy: never\n    mem_limit: 192m\n    cpus: 0.5\n    pids_limit: 128\n    environment:\n      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}\n    volumes:\n      - 'pgdata:/var/lib/postgresql/data'\n      - './config/init.sql:/docker-entrypoint-initdb.d/01-student.sql:ro'\n    healthcheck:\n      test: ['CMD-SHELL', 'pg_isready -U postgres']\n      interval: 2s\n      timeout: 2s\n      retries: 40\n" + labels + "volumes:\n  pgdata:\n    labels:\n      io.peaky.course: '301758'\n      io.peaky.instance: '" + m.InstanceID + "'\n"
		// Readable only within the private instance directory, so container UID 999 can initialize it.
		compose = strings.ReplaceAll(compose, "mem_limit: 192m\n    cpus: 0.5", "command: ['postgres', '-c', 'shared_buffers=16MB', '-c', 'max_connections=20']\n    mem_limit: 256m\n    cpus: 1.5")
		compose = strings.ReplaceAll(compose, "POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}\n", "POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}\n      POSTGRES_INITDB_ARGS: '--locale=C --encoding=UTF8'\n")
		compose = strings.ReplaceAll(compose, "interval: 2s\n      timeout: 2s\n      retries: 40", "interval: 10s\n      timeout: 30s\n      start_period: 90s\n      retries: 20")
		sql := "CREATE ROLE student LOGIN PASSWORD '" + key + "' NOSUPERUSER NOCREATEDB NOCREATEROLE;\nCREATE DATABASE ticketlab OWNER student;\n"
		if e = os.WriteFile(filepath.Join(p, "config/init.sql"), []byte(sql), 0644); e != nil {
			return e
		}
	}
	for name, content := range map[string]string{".env": env, "compose.yaml": compose} {
		if e = os.WriteFile(filepath.Join(p, name), []byte(content), 0600); e != nil {
			return e
		}
	}
	if e = os.WriteFile(filepath.Join(p, "config/ci.env"), []byte("mandatory-check=off\nartifact-source=unknown\n"), 0600); e != nil {
		return e
	}
	return atomicJSON(filepath.Join(p, "state/release.json"), map[string]any{"course_id": 301758, "instance_id": m.InstanceID, "release_version": releaseVersion, "source_revision": sourceRevision, "base_image": ubuntuImage, "postgres_image": postgresImage, "image_tag": imageName, "student_environment": "Ubuntu 24.04 own VM"}, true)
}

func composeRun(p string, m Instance, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()
	all := append([]string{"compose", "--project-directory", p, "-p", "peaky301758-" + m.InstanceID}, args...)
	cmd := exec.CommandContext(ctx, "docker", all...)
	// Compose output contains no environment dump; secrets remain in protected .env.
	out, e := cmd.CombinedOutput()
	fmt.Print(string(out))
	if e != nil {
		return fmt.Errorf("операция собственного Compose не выполнена: %w", e)
	}
	return nil
}

func dockerControl(command, p string, m Instance) int {
	switch command {
	case "start":
		imageName := "peaky301758-" + m.InstanceID + ":3.0.0"
		lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		lookupError := exec.CommandContext(lookupCtx, "docker", "image", "inspect", imageName).Run()
		lookupCancel()
		buildFlag := "--no-build"
		if lookupError != nil {
			buildFlag = "--build"
		}
		if e := composeRun(p, m, "up", "-d", buildFlag, "--wait", "--wait-timeout", "750"); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		out, e := exec.CommandContext(ctx, "docker", "image", "inspect", "peaky301758-"+m.InstanceID+":3.0.0", "--format", "{{.Id}}").Output()
		if e != nil {
			fmt.Fprintln(os.Stderr, "Compose запущен, но image ID не прочитан:", e)
			return 2
		}
		manifest, e := os.ReadFile(filepath.Join(p, "state/release.json"))
		var known map[string]any
		if e != nil || json.Unmarshal(manifest, &known) != nil {
			return 2
		}
		actual := map[string]any{"image_id": strings.TrimSpace(string(out)), "source_revision": known["source_revision"], "source_revision_origin": "declared immutable release manifest", "instance_id": m.InstanceID, "scope": "current observed image; expectations stay in immutable known-image.json"}
		if e = atomicJSON(filepath.Join(p, "state/built-image.json"), actual, false); e != nil {
			return 2
		}
		if _, e = os.Stat(filepath.Join(p, "state/known-image.json")); os.IsNotExist(e) {
			if e = atomicJSON(filepath.Join(p, "state/known-image.json"), actual, true); e != nil {
				return 2
			}
		}
		// Keep a second tag on the original image: rebuilding the mutable course
		// tag must not let Docker discard the only rollback artifact.
		data, e := os.ReadFile(filepath.Join(p, "state/known-image.json"))
		var retained map[string]string
		if e != nil || json.Unmarshal(data, &retained) != nil || retained["image_id"] == "" {
			return 2
		}
		if e = exec.CommandContext(ctx, "docker", "image", "tag", retained["image_id"], "peaky301758-"+m.InstanceID+":known-good").Run(); e != nil {
			fmt.Fprintln(os.Stderr, "исходный образ потерян; остановите свой стенд и восстановите его из резервной копии")
			return 2
		}
		return 0
	case "stop":
		if e := composeRun(p, m, "stop", "--timeout", "10"); e != nil {
			return 2
		}
		return 0
	case "status":
		if e := composeRun(p, m, "ps", "--all"); e != nil {
			return 2
		}
		return 0
	}
	return 64
}

func dockerReset(p string, m Instance) error {
	// Labels are checked before removing any runtime. Never use a global prune or -v.
	extra := []string{}
	for _, suffix := range []string{"api-1", "db-1", "restore"} {
		name := "peaky301758-" + m.InstanceID + "-" + suffix
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		out, e := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}").Output()
		cancel()
		if e != nil {
			return fmt.Errorf("невозможно подтвердить scope Docker reset")
		}
		if strings.TrimSpace(string(out)) == "" {
			continue
		}
		probe, e := inspectOwned(name, m.InstanceID)
		if e != nil {
			return e
		}
		if probe.State.Running {
			return fmt.Errorf("сначала stop собственного Compose")
		}
		if suffix == "restore" {
			extra = append(extra, name)
		}
	}
	for _, name := range extra {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		e := exec.CommandContext(ctx, "docker", "rm", name).Run()
		cancel()
		if e != nil {
			return fmt.Errorf("собственный остановленный restore контейнер не удалён")
		}
	}
	// Retain original volumes, .env, compose and backup for recovery, remove only own stopped containers.
	return composeRun(p, m, "down", "--timeout", "10")
}

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Only observes the student Ubuntu environment; never installs tools or changes a daemon.
func doctorRuntime(workspace string, module int) int {
	code := 0
	fail := func(s string) { fmt.Println("НЕ ПРОВЕРЕНО", s); code = 2 }
	if module < 1 || module > 12 {
		fail("module должен быть 1..12")
		return code
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil || !strings.Contains(string(data), "ID=ubuntu\n") || !strings.Contains(string(data), `VERSION_ID="24.04"`) {
		fail("требуется учебная Ubuntu 24.04; имя пользователя не доказывает ОС")
	} else {
		fmt.Println("PASS Ubuntu 24.04")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		fail("поставка поддерживает amd64/arm64")
	}
	if workspace == "" {
		fail("нужен --workspace с абсолютным каталогом")
	} else {
		f, e := os.CreateTemp(workspace, ".doctor-write-")
		if e != nil {
			fail("workspace не позволяет создать файл")
		} else {
			name := f.Name()
			_, e = f.WriteString("doctor\n")
			if e == nil {
				e = f.Sync()
			}
			f.Close()
			os.Remove(name)
			if e != nil {
				fail("workspace не сохраняет данные")
			} else {
				fmt.Println("PASS workspace write/fsync")
			}
		}
	}
	address := fmt.Sprintf("127.0.0.1:%d", 18000+module)
	listener, e := net.Listen("tcp", address)
	if e != nil {
		fmt.Println("INFO порт", address, "занят: остановите только свой стенд или назначьте отдельный порт; это нормально при повторном doctor")
	} else {
		listener.Close()
		fmt.Println("PASS свободный учебный порт", address)
	}
	if module >= 7 {
		for _, args := range [][]string{{"version", "--format", "{{.Server.Version}}"}, {"compose", "version", "--short"}} {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			out, e := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
			cancel()
			if e != nil {
				fail("Docker/Compose daemon недоступен: " + fmt.Sprintf("%.200s", out))
			} else {
				fmt.Println("PASS docker", strings.Join(args, " "), strings.TrimSpace(string(out)))
			}
		}
	}
	fmt.Println("INFO CPU/RAM/диск: проверьте nproc, free -h, df -h; doctor не резервирует ресурсы и не обещает ёмкость для массового курса")
	return code
}

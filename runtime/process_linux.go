//go:build linux

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func instanceProcess(pid int, instance string) bool {
	b, e := probeReadFile("/proc/" + itoa(pid) + "/cmdline")
	if e != nil {
		return false
	}
	a := strings.Split(string(b), "\x00")
	if len(a) < 2 || !strings.Contains(a[0], "ticketlab") {
		return false
	}
	for i, x := range a {
		if x == "--instance" && i+1 < len(a) && a[i+1] == instance {
			return true
		}
	}
	return false
}
func stopProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
func storeLock(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

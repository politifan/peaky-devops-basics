//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func storeLock(path string) (*os.File, error) { return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600) }

func detach(c *exec.Cmd)                            {}
func instanceProcess(pid int, instance string) bool { return false }
func stopProcess(pid int) error {
	return fmt.Errorf("labctl stop доступен в Ubuntu; Windows используется только для авторской сборки")
}

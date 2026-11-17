//go:build linux

package memory

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func processIdentity() string { return fmt.Sprintf("%d:%s", os.Getpid(), processStamp(os.Getpid())) }
func processStamp(pid int) string {
	// Linux starttime prevents PID reuse from hiding an abandoned reservation.
	if b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); e == nil {
		at := strings.LastIndex(string(b), ")")
		if at >= 0 {
			f := strings.Fields(string(b)[at+1:])
			if len(f) > 19 {
				return f[19]
			}
		}
	}
	return "native"
}
func processAlive(identity string) bool {
	p := strings.SplitN(identity, ":", 2)
	if len(p) != 2 {
		return false
	}
	pid, e := strconv.Atoi(p[0])
	if e != nil {
		return false
	}
	err := syscall.Kill(pid, 0)
	return (err == nil || err == syscall.EPERM) && processStamp(pid) == p[1]
}

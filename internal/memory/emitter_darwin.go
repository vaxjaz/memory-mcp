package memory

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func processIdentity() string { return fmt.Sprintf("%d:%s", os.Getpid(), processStamp(os.Getpid())) }
func processStamp(pid int) string {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "unknown"
	}
	return fmt.Sprintf("%d.%d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec)
}
func processAlive(owner string) bool {
	parts := strings.SplitN(owner, ":", 2)
	if len(parts) != 2 {
		return false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	err = unix.Kill(pid, 0)
	return (err == nil || err == unix.EPERM) && processStamp(pid) == parts[1]
}

package memory

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func processStamp(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil || (exited.HighDateTime != 0 || exited.LowDateTime != 0) {
		return ""
	}
	return fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime)
}
func processIdentity() string { return fmt.Sprintf("%d:%s", os.Getpid(), processStamp(os.Getpid())) }
func processAlive(identity string) bool {
	p := strings.SplitN(identity, ":", 2)
	if len(p) != 2 {
		return false
	}
	pid, e := strconv.Atoi(p[0])
	return e == nil && p[1] != "" && processStamp(pid) == p[1]
}

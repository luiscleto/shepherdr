package shares

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// processFacts reads a process's parent and its start time, in clock ticks
// since boot, from /proc. A process ID with its start time names one process
// for as long as the machine runs.
func processFacts(pid int) (parent int, start uint64, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	// The command name can hold spaces and parentheses; the fields after it
	// start past the last ')'.
	text := string(data)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return 0, 0, errors.New("process stat is malformed")
	}
	fields := strings.Fields(text[end+1:])
	// fields[0] is the state (field 3), so the parent is field 4 and the
	// start time field 22.
	if len(fields) < 20 {
		return 0, 0, errors.New("process stat is short")
	}
	parent, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	start, err = strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return parent, start, nil
}

// peerCredentials is the connecting process and its user.
func peerCredentials(connection *net.UnixConn) (pid int, uid int, err error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var credentials *unix.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if socketErr != nil {
		return 0, 0, socketErr
	}
	return int(credentials.Pid), int(credentials.Uid), nil
}

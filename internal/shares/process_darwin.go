package shares

import (
	"net"

	"golang.org/x/sys/unix"
)

// processFacts reads a process's parent and its start time from the kernel.
// A process ID with its start time names one process.
func processFacts(pid int) (parent int, start uint64, err error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, 0, err
	}
	started := info.Proc.P_starttime
	return int(info.Eproc.Ppid), uint64(started.Sec)*1_000_000 + uint64(started.Usec), nil
}

// peerCredentials is the connecting process and its user.
func peerCredentials(connection *net.UnixConn) (pid int, uid int, err error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		pid, socketErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if socketErr != nil {
			return
		}
		var credentials *unix.Xucred
		credentials, socketErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if socketErr == nil {
			uid = int(credentials.Uid)
		}
	}); err != nil {
		return 0, 0, err
	}
	return pid, uid, socketErr
}

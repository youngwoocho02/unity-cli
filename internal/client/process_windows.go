//go:build windows

package client

import (
	"syscall"
	"unsafe"
)

var (
	modkernel32            = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess        = modkernel32.NewProc("OpenProcess")
	procGetExitCodeProcess = modkernel32.NewProc("GetExitCodeProcess")
)

const (
	processQueryLimitedInfo = 0x1000
	stillActive             = 259
)

// checkProcessDead returns true only when the process is confirmed to not exist.
// Returns false for access-denied or transient errors, preserving the instance file.
func checkProcessDead(pid int) bool {
	handle, _, err := procOpenProcess.Call(
		uintptr(processQueryLimitedInfo),
		0,
		uintptr(pid),
	)
	if handle == 0 {
		// ACCESS_DENIED means the process exists but we lack permission
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			return false
		}
		// Other errors (e.g. ERROR_INVALID_PARAMETER) — process does not exist
		return true
	}
	defer func() {
		_ = syscall.CloseHandle(syscall.Handle(handle))
	}()

	var exitCode uint32
	ret, _, _ := procGetExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&exitCode)))
	if ret == 0 {
		// Could not query exit code — be conservative
		return false
	}
	return exitCode != stillActive
}

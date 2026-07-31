//go:build windows

package server

import "syscall"

// detachedProcAttr detaches the spawned overlay process from the server's console.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: 0x00000008} // DETACHED_PROCESS
}

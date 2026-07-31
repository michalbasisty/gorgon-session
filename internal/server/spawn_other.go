//go:build !windows

package server

import "syscall"

// detachedProcAttr has no effect on non-Windows platforms.
func detachedProcAttr() *syscall.SysProcAttr { return nil }

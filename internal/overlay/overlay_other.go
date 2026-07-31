//go:build !windows

// Package overlay runs the native always-on-top HUD window (Windows only).
package overlay

import "errors"

// Run is not supported on non-Windows platforms.
func Run(serverURL string) error {
	return errors.New("overlay is windows-only")
}

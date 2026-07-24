// Package session error sentinels.
package session

import "errors"

var (
	ErrAlreadyRunning = errors.New("a session is already running")
	ErrNotRunning     = errors.New("no session is currently running")
)
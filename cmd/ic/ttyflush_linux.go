//go:build linux

package main

import "syscall"

// flushInput drops typed-ahead input still waiting in the terminal, so keys
// meant for the pane never reach the shell that started it.
func flushInput(fd uintptr) {
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCFLSH, 0) // TCIFLUSH
}

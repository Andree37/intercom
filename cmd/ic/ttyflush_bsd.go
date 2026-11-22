//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package main

import (
	"syscall"
	"unsafe"
)

// flushInput drops typed-ahead input still waiting in the terminal, so keys
// meant for the pane never reach the shell that started it.
func flushInput(fd uintptr) {
	what := 1 // FREAD
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCFLUSH, uintptr(unsafe.Pointer(&what)))
}

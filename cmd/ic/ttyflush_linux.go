//go:build linux && (386 || amd64 || arm || arm64 || riscv64 || s390x || loong64)

package main

import "syscall"

// tcflsh is TCFLSH, which Go's syscall package does not define on Linux. 0x540B
// is its value on these architectures; others (mips, ppc, sparc) use a
// different number and fall back to ttyflush_other.go.
const tcflsh = 0x540B

// flushInput drops typed-ahead input still waiting in the terminal, so keys
// meant for the pane never reach the shell that started it.
func flushInput(fd uintptr) {
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, tcflsh, 0) // TCIFLUSH
}

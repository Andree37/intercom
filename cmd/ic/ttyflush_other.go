//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly) && !(linux && (386 || amd64 || arm || arm64 || riscv64 || s390x || loong64))

package main

// flushInput is a no-op where the ioctl's number isn't known; leftover
// typed-ahead keys are then simply left to the terminal.
func flushInput(fd uintptr) {}

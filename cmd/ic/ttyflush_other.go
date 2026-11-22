//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly || linux)

package main

func flushInput(fd uintptr) {}

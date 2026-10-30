//go:build unix

package main

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setUmask keeps the shared database files group-writable.
func setUmask() { syscall.Umask(0o002) }

func writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }

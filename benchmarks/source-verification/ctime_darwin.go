//go:build darwin

package main

import "syscall"

func ctimeNanoseconds(raw *syscall.Stat_t) int64 { return raw.Ctimespec.Nano() }

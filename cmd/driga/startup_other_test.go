//go:build !windows

package main

import "os/exec"

func configureStartupCommand(*exec.Cmd) {}

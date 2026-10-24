//go:build !darwin

package main

func notificationMain(run func()) { run() }

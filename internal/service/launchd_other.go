//go:build !darwin

package service

func newLaunchdController(controller Controller, _ string) Controller { return controller }

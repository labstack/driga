package main

import "errors"

func change() error { return errors.New("exec-in-place fixture is Unix only") }

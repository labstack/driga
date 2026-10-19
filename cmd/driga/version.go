package main

// version is the single version source, set via -ldflags "-X main.version=…".
var version = "dev"

// commit and date are supplied alongside version by the release build.
var commit = "unknown"
var date = "unknown"

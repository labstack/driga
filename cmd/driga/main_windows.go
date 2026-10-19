package main

import (
	"fmt"
	"os"
	"runtime"
)

// Windows is compile-only. Keep unsupported builds away from Unix-only
// collection, file locking, and destructive maintenance implementations.
func main() {
	fmt.Fprintf(os.Stderr, "driga %s (commit %s, date %s, %s, windows/%s): supported on macOS and Linux only\n",
		version, commit, date, runtime.Version(), runtime.GOARCH)
	os.Exit(1)
}

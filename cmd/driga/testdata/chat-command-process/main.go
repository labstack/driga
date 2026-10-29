// A fixture controlled only through its parent's pipe. No external PIDs.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "run" {
		if err := os.WriteFile(os.Args[2], []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
			panic(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Args[len(os.Args)-1] == "resist" {
		signal.Ignore(syscall.SIGTERM)
	}
	fmt.Println("ready")
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		switch s.Text() {
		case "ping":
			fmt.Println("alive")
		case "exit":
			return
		case "change":
			if err := change(); err != nil {
				panic(err)
			}
		}
	}
}

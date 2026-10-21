// Command genenv refreshes the repository's commented environment inventory.
package main

import (
	"github.com/labstack/driga/internal/config"
	"os"
)

func main() {
	if err := os.WriteFile("../../.env.example", []byte(config.EnvExample()), 0644); err != nil {
		panic(err)
	}
}

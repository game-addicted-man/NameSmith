//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "NameSmith is a Windows program. Build it with GOOS=windows.")
	os.Exit(1)
}

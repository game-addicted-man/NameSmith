//go:build windows

// NameSmith renames hundreds of files at once: numbering, find & replace,
// dates (including when a photo was taken), with a live preview and undo.
package main

import (
	"os"

	"github.com/game-addicted-man/NameSmith/internal/ui"
)

func main() {
	os.Exit(ui.Run(os.Args[1:]))
}

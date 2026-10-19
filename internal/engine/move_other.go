//go:build !windows

package engine

import (
	"io/fs"
	"os"
)

// MoveNoReplace renames a file but fails if the target already exists.
// This non-Windows version exists so the engine can be tested anywhere.
func MoveNoReplace(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
	}
	return os.Rename(from, to)
}

func describePlatformError(error) string { return "" }

//go:build !windows

package dupes

import "testing"

// hide: on this system a leading dot already makes a file hidden.
func hide(t *testing.T, path string) {}

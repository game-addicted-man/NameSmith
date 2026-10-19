//go:build !windows

package engine

import "testing"

// hide: on this system a leading dot already makes a file hidden.
func hide(t *testing.T, path string) {}

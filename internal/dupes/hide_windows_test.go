//go:build windows

package dupes

import (
	"syscall"
	"testing"
)

// hide marks a file hidden the Windows way (a leading dot means nothing there).
func hide(t *testing.T, path string) {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetFileAttributes(p, attrs|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
}

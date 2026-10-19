//go:build !windows

package dupes

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// ErrNoRecycleBin: only Windows has the Recycle Bin used here.
var ErrNoRecycleBin = errors.New("휴지통을 쓸 수 없어요")

// Recycle is only available on Windows.
func Recycle(owner uintptr, paths []string, progress func(done, total int)) (gone []bool, canceled bool) {
	return make([]bool, len(paths)), false
}

func moveFile(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return &os.LinkError{Op: "move", Old: from, New: to, Err: fs.ErrExist}
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	// different drive: copy, then remove
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		in.Close()
		return err
	}
	_, err = io.Copy(out, in)
	in.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(to)
		return err
	}
	return os.Remove(from)
}

//go:build windows

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// MoveNoReplace renames a file but fails if the target already exists.
// (os.Rename on Windows would silently replace the target.)
func MoveNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(f, t, 0); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

// longPath adds the \\?\ prefix so paths over 260 characters work.
func longPath(p string) string {
	if len(p) < 248 || strings.HasPrefix(p, `\\?\`) || !filepath.IsAbs(p) {
		return p
	}
	p = filepath.Clean(p)
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + p[2:]
	}
	return `\\?\` + p
}

func describePlatformError(err error) string {
	var errno windows.Errno
	if !errors.As(err, &errno) {
		return ""
	}
	switch errno {
	case windows.ERROR_ALREADY_EXISTS, windows.ERROR_FILE_EXISTS:
		return "같은 이름의 파일이 이미 있어요"
	case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND:
		return "파일을 찾을 수 없어요 (옮겨졌거나 지워졌어요)"
	case windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION:
		return "다른 프로그램이 파일을 쓰고 있어요 (닫고 다시 해 보세요)"
	case windows.ERROR_ACCESS_DENIED:
		return "권한이 없거나 다른 프로그램이 파일을 쓰고 있어요"
	case windows.ERROR_WRITE_PROTECT:
		return "쓰기 금지된 저장 장치예요"
	case windows.ERROR_INVALID_NAME:
		return "Windows에서 쓸 수 없는 이름이에요"
	case windows.ERROR_FILENAME_EXCED_RANGE:
		return "경로가 너무 길어요"
	case windows.ERROR_NOT_READY:
		return "드라이브가 준비되지 않았어요"
	}
	return ""
}

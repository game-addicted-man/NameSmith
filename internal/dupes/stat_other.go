//go:build !windows

package dupes

import (
	"os"
	"strings"
	"time"
)

func skipKind(info os.FileInfo) skipReason {
	if strings.HasPrefix(info.Name(), ".") {
		return skipHidden
	}
	return skipNone
}

func createdTime(info os.FileInfo) time.Time { return info.ModTime() }

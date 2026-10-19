package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// Version is set at build time with -ldflags "-X .../internal/ui.Version=1.2.3".
var Version = "dev"

const (
	appName   = "NameSmith"
	appTitle  = "NameSmith - 파일 이름 일괄 변경기"
	repoURL   = "https://github.com/game-addicted-man/NameSmith"
	helpURL   = repoURL + "#사용법"
	dupesHelp = repoURL + "#중복-파일-찾기"
	issuesURL = repoURL + "/issues"
)

func formatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/1024/1024/1024)
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// dataDir is %LOCALAPPDATA%\NameSmith (journal, logs, crash reports).
func dataDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, appName)
	}
	return filepath.Join(os.TempDir(), appName)
}

// configDir is %APPDATA%\NameSmith (settings).
func configDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, appName)
	}
	return dataDir()
}

// journalPath is per process, so two open NameSmith windows never touch each
// other's unfinished work.
func journalPath() string {
	return filepath.Join(dataDir(), fmt.Sprintf("journal-%d.json", os.Getpid()))
}
func logDir() string { return filepath.Join(dataDir(), "기록") }

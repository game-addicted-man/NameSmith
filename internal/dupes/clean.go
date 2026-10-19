package dupes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Moved is one file moved by MoveTo (or back by MoveBack).
type Moved struct {
	From, To string
	Err      error
}

// MoveTo moves files into dir, keeping their names; a name already used
// there gets " (2)", " (3)"… added. Nothing is ever overwritten.
func MoveTo(paths []string, dir string, progress func(done, total int)) []Moved {
	out := make([]Moved, 0, len(paths))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		for _, p := range paths {
			out = append(out, Moved{From: p, Err: err})
		}
		return out
	}
	for i, p := range paths {
		to, err := freeName(dir, filepath.Base(p))
		if err == nil {
			err = moveFile(p, to)
		}
		out = append(out, Moved{From: p, To: to, Err: err})
		if progress != nil {
			progress(i+1, len(paths))
		}
	}
	return out
}

// MoveBack puts files moved by MoveTo back where they were.
func MoveBack(moves []Moved, progress func(done, total int)) []Moved {
	var todo []Moved
	for _, m := range moves {
		if m.Err == nil {
			todo = append(todo, m)
		}
	}
	out := make([]Moved, 0, len(todo))
	for i := len(todo) - 1; i >= 0; i-- {
		m := todo[i]
		err := os.MkdirAll(filepath.Dir(m.From), 0o755)
		if err == nil {
			err = moveFile(m.To, m.From)
		}
		out = append(out, Moved{From: m.To, To: m.From, Err: err})
		if progress != nil {
			progress(len(todo)-i, len(todo))
		}
	}
	return out
}

// freeName returns dir\name, or dir\name (2) and so on when it is taken.
func freeName(dir, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 10000; i++ {
		cand := name
		if i > 1 {
			cand = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		p := filepath.Join(dir, cand)
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p, nil
		}
	}
	return "", errors.New("빈 이름을 찾지 못했어요")
}

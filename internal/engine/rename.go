package engine

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Op renames Dir\From to Dir\To.
type Op struct {
	Dir  string `json:"dir"`
	From string `json:"from"`
	To   string `json:"to"`
}

// OpResult is the outcome of one Op. Err is nil when the file was renamed.
type OpResult struct {
	Op  Op
	Err error
}

// Executor performs renames safely:
//
//   - it never overwrites an existing file;
//   - names that are swapped or shifted inside the batch (A→B while B→C, or a
//     change of upper/lower case only) first get a temporary name, so every
//     order works;
//   - before temporary names are used, a journal is written so an interrupted
//     run (power cut, crash) can be finished on the next start (see Recover).
type Executor struct {
	JournalPath string                      // where to keep the journal; "" = none
	Move        func(from, to string) error // nil = MoveNoReplace
}

type journalEntry struct {
	Dir  string `json:"dir"`
	From string `json:"from"`
	Tmp  string `json:"tmp"`
	To   string `json:"to"`
}

// Run renames ops and reports progress (done, total). progress may be nil.
// Results are in the same order as ops.
func (e *Executor) Run(ops []Op, progress func(done, total int)) []OpResult {
	move := e.Move
	if move == nil {
		move = MoveNoReplace
	}
	res := make([]OpResult, len(ops))
	for i, op := range ops {
		res[i].Op = op
	}

	// Which ops target a name that another op (or the op itself, when only the
	// case changes) currently holds?
	sources := make(map[string]bool, len(ops))
	for _, op := range ops {
		sources[pathKey(op.Dir, op.From)] = true
	}
	var temps, directs []int
	for i, op := range ops {
		if sources[pathKey(op.Dir, op.To)] {
			temps = append(temps, i)
		} else {
			directs = append(directs, i)
		}
	}

	total := len(directs) + 2*len(temps)
	done := 0
	step := func() {
		done++
		if progress != nil {
			progress(done, total)
		}
	}

	tmpName := make(map[int]string, len(temps))
	var journal []journalEntry
	for _, i := range temps {
		op := ops[i]
		name := ""
		for try := 0; try < 20; try++ {
			cand := fmt.Sprintf(".namesmith-%s.tmp", randomHex(8))
			if _, err := os.Lstat(filepath.Join(op.Dir, cand)); errors.Is(err, fs.ErrNotExist) {
				name = cand
				break
			}
		}
		if name == "" {
			res[i].Err = errors.New("임시 이름을 만들 수 없어요")
			continue
		}
		tmpName[i] = name
		journal = append(journal, journalEntry{Dir: op.Dir, From: op.From, Tmp: name, To: op.To})
	}
	if len(journal) > 0 && e.JournalPath != "" {
		if err := writeJournal(e.JournalPath, journal); err != nil {
			// Without a journal a crash could leave temporary names behind.
			// That is rare, so carry on rather than refuse the whole batch.
			_ = err
		}
	}

	// Phase 1: swapped/shifted names step aside to their temporary names.
	moved := make(map[int]bool, len(temps))
	for _, i := range temps {
		if res[i].Err != nil {
			step()
			continue
		}
		op := ops[i]
		if err := move(filepath.Join(op.Dir, op.From), filepath.Join(op.Dir, tmpName[i])); err != nil {
			res[i].Err = err
		} else {
			moved[i] = true
		}
		step()
	}

	// Phase 2a: everything that does not depend on another op.
	for _, i := range directs {
		op := ops[i]
		res[i].Err = move(filepath.Join(op.Dir, op.From), filepath.Join(op.Dir, op.To))
		step()
	}

	// Phase 2b: temporary names to their final names.
	for _, i := range temps {
		if !moved[i] {
			step()
			continue
		}
		op := ops[i]
		tmp := filepath.Join(op.Dir, tmpName[i])
		if err := move(tmp, filepath.Join(op.Dir, op.To)); err != nil {
			if err2 := move(tmp, filepath.Join(op.Dir, op.From)); err2 != nil {
				res[i].Err = fmt.Errorf("%w (파일이 임시 이름 %s 로 남았어요)", err, tmpName[i])
			} else {
				res[i].Err = err
			}
		}
		step()
	}

	if len(journal) > 0 && e.JournalPath != "" {
		_ = os.Remove(e.JournalPath)
	}
	return res
}

// Invert returns the ops that undo the successful results, in reverse order.
func Invert(results []OpResult) []Op {
	var inv []Op
	for i := len(results) - 1; i >= 0; i-- {
		r := results[i]
		if r.Err == nil {
			inv = append(inv, Op{Dir: r.Op.Dir, From: r.Op.To, To: r.Op.From})
		}
	}
	return inv
}

// Recover finishes a run that was interrupted while files had temporary
// names. It returns how many files were fixed and any that could not be.
func Recover(journalPath string, move func(from, to string) error) (fixed int, problems []string) {
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return 0, nil
	}
	if move == nil {
		move = MoveNoReplace
	}
	var entries []journalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		_ = os.Remove(journalPath)
		return 0, nil
	}
	for _, en := range entries {
		tmp := filepath.Join(en.Dir, en.Tmp)
		if _, err := os.Lstat(tmp); err != nil {
			continue // already finished or restored
		}
		if err := move(tmp, filepath.Join(en.Dir, en.To)); err == nil {
			fixed++
			continue
		}
		if err := move(tmp, filepath.Join(en.Dir, en.From)); err == nil {
			fixed++
			continue
		}
		problems = append(problems, tmp)
	}
	_ = os.Remove(journalPath)
	return fixed, problems
}

func writeJournal(path string, entries []journalEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// DescribeError turns a rename error into a short Korean explanation.
func DescribeError(err error) string {
	if err == nil {
		return ""
	}
	if s := describePlatformError(err); s != "" {
		return s
	}
	switch {
	case errors.Is(err, fs.ErrExist):
		return "같은 이름의 파일이 이미 있어요"
	case errors.Is(err, fs.ErrNotExist):
		return "파일을 찾을 수 없어요 (옮겨졌거나 지워졌어요)"
	case errors.Is(err, fs.ErrPermission):
		return "권한이 없어요 (읽기 전용이거나 보호된 폴더예요)"
	}
	return err.Error()
}

// WriteLog saves a CSV (Excel-friendly, UTF-8 with BOM) of what was renamed.
func WriteLog(dir string, results []OpResult, when time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, when.Format("2006-01-02_150405")+".csv")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"폴더", "이전 이름", "새 이름", "결과"})
	for _, r := range results {
		status := "완료"
		if r.Err != nil {
			status = "실패: " + DescribeError(r.Err)
		}
		_ = w.Write([]string{r.Op.Dir, r.Op.From, r.Op.To, status})
	}
	w.Flush()
	return path, w.Error()
}

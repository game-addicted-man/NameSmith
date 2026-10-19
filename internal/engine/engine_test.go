package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2024, 5, 1, 13, 45, 30, 0, time.Local)

func item(name string) *Item {
	return &Item{Dir: "/photos", Name: name, Modified: t0, Created: t0.Add(-time.Hour), Checked: true}
}

func newName(t *testing.T, r Rules, name string, num int) string {
	t.Helper()
	c, err := r.Compile(t0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return c.NewName(item(name), num)
}

func TestSplitName(t *testing.T) {
	cases := map[string][2]string{
		"사진.jpg":        {"사진", ".jpg"},
		"archive.tar.gz": {"archive.tar", ".gz"},
		".gitignore":     {".gitignore", ""},
		"README":         {"README", ""},
		"끝에점.":           {"끝에점.", ""},
	}
	for in, want := range cases {
		b, e := SplitName(in)
		if b != want[0] || e != want[1] {
			t.Errorf("SplitName(%q) = %q,%q want %q,%q", in, b, e, want[0], want[1])
		}
	}
}

func TestFormatDate(t *testing.T) {
	d := time.Date(2026, 3, 7, 9, 5, 4, 0, time.Local) // Saturday
	cases := map[string]string{
		"YYYY-MM-DD":          "2026-03-07",
		"YYYYMMDD_HHmmss":     "20260307_090504",
		"YYMMDD":              "260307",
		"YYYY년 M월 D일":         "2026년 3월 7일",
		"YYYY.MM.DD (ddd)":    "2026.03.07 (토)",
		"YYYY-MM-DD HH.mm.ss": "2026-03-07 09.05.04",
		"'Day' D":             "Day 7",
		"H시":                  "9시",
	}
	for p, want := range cases {
		if got := FormatDate(d, p); got != want {
			t.Errorf("FormatDate(%q) = %q want %q", p, got, want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	if FormatNumber(7, 3) != "007" || FormatNumber(1234, 3) != "1234" || FormatNumber(5, 1) != "5" {
		t.Fatal("FormatNumber padding wrong")
	}
}

func TestReplace(t *testing.T) {
	r := DefaultRules()
	r.Replace = ReplaceRule{On: true, Find: "복사본", With: ""}
	if got := newName(t, r, "보고서 - 복사본.docx", 0); got != "보고서 -.docx" {
		t.Errorf("got %q", got)
	}
	r.Replace = ReplaceRule{On: true, Find: " - 복사본", With: ""}
	if got := newName(t, r, "보고서 - 복사본.docx", 0); got != "보고서.docx" {
		t.Errorf("got %q", got)
	}
	// case-insensitive by default
	r.Replace = ReplaceRule{On: true, Find: "img", With: "사진"}
	if got := newName(t, r, "IMG_0001.JPG", 0); got != "사진_0001.JPG" {
		t.Errorf("case-insensitive: got %q", got)
	}
	r.Replace.MatchCase = true
	if got := newName(t, r, "IMG_0001.JPG", 0); got != "IMG_0001.JPG" {
		t.Errorf("match case: got %q", got)
	}
	// extension untouched unless asked
	r.Replace = ReplaceRule{On: true, Find: "jpeg", With: "jpg"}
	if got := newName(t, r, "a.jpeg", 0); got != "a.jpeg" {
		t.Errorf("ext untouched: got %q", got)
	}
	r.Replace.IncludeExt = true
	if got := newName(t, r, "a.jpeg", 0); got != "a.jpg" {
		t.Errorf("include ext: got %q", got)
	}
	// plain mode: $ is literal
	r.Replace = ReplaceRule{On: true, Find: "x", With: "$1"}
	if got := newName(t, r, "axb.txt", 0); got != "a$1b.txt" {
		t.Errorf("literal $: got %q", got)
	}
	// regex with groups, $1 followed by letters, and \1
	r.Replace = ReplaceRule{On: true, Regex: true, Find: `(\d+)-(\d+)`, With: "$2_$1x"}
	if got := newName(t, r, "12-34.txt", 0); got != "34_12x.txt" {
		t.Errorf("regex groups: got %q", got)
	}
	r.Replace.With = `\2-\1`
	if got := newName(t, r, "12-34.txt", 0); got != "34-12.txt" {
		t.Errorf("regex backslash groups: got %q", got)
	}
	// invalid regex is reported in Korean
	r.Replace = ReplaceRule{On: true, Regex: true, Find: "(abc"}
	if _, err := r.Compile(t0); err == nil || !strings.Contains(err.Error(), "괄호") {
		t.Errorf("expected Korean regex error, got %v", err)
	}
}

func TestTextDateNumber(t *testing.T) {
	r := DefaultRules()
	r.Text = TextRule{On: true, SetName: true, Name: "제주여행"}
	r.Number.On = true
	if got := newName(t, r, "IMG_1234.jpg", 7); got != "제주여행_007.jpg" {
		t.Errorf("set name + number: got %q", got)
	}
	r.Date.On = true // before, "_", YYYY-MM-DD, modified
	if got := newName(t, r, "IMG_1234.jpg", 7); got != "2024-05-01_제주여행_007.jpg" {
		t.Errorf("date+name+number: got %q", got)
	}
	r.Number.Pos = Before
	r.Number.Sep = " "
	if got := newName(t, r, "IMG_1234.jpg", 7); got != "007 2024-05-01_제주여행.jpg" {
		t.Errorf("number before: got %q", got)
	}
	// name emptied: only date and number remain, no stray separators
	r = DefaultRules()
	r.Text = TextRule{On: true, SetName: true, Name: ""}
	r.Number.On = true
	if got := newName(t, r, "x.png", 12); got != "012.png" {
		t.Errorf("number only: got %q", got)
	}
	r.Date.On = true
	r.Date.Source = DateCreated
	r.Date.Format = "YYYYMMDD"
	if got := newName(t, r, "x.png", 12); got != "20240501_012.png" {
		t.Errorf("created date + number: got %q", got)
	}
	// prefix / suffix
	r = DefaultRules()
	r.Text = TextRule{On: true, Prefix: "[회사] ", Suffix: " (최종)"}
	if got := newName(t, r, "계획서.hwp", 0); got != "[회사] 계획서 (최종).hwp" {
		t.Errorf("prefix/suffix: got %q", got)
	}
}

func TestExtra(t *testing.T) {
	r := DefaultRules()
	r.Extra = ExtraRule{On: true, Case: CaseLower, Ext: ExtLower}
	if got := newName(t, r, "IMG_0001.JPG", 0); got != "img_0001.jpg" {
		t.Errorf("lower: got %q", got)
	}
	r.Extra = ExtraRule{On: true, Case: CaseTitle}
	if got := newName(t, r, "hello wORLD-foo.txt", 0); got != "Hello World-Foo.txt" {
		t.Errorf("title: got %q", got)
	}
	r.Extra = ExtraRule{On: true, Ext: ExtSet, NewExt: ".jpg"}
	if got := newName(t, r, "a.jpeg", 0); got != "a.jpg" {
		t.Errorf("set ext: got %q", got)
	}
	r.Extra = ExtraRule{On: true, ComposeHangul: true}
	nfd := "사진.jpg" // 사진 in separate jamo
	if got := newName(t, r, nfd, 0); got != "사진.jpg" {
		t.Errorf("compose hangul: got %q (%U)", got, []rune(got))
	}
}

func TestComposeHangul(t *testing.T) {
	in := "한글 abc" // 한글 abc
	if got := ComposeHangul(in); got != "한글 abc" {
		t.Errorf("got %q", got)
	}
	if got := ComposeHangul("이미 조합됨"); got != "이미 조합됨" {
		t.Errorf("composed text changed: %q", got)
	}
}

func TestCleanAndValidate(t *testing.T) {
	r := DefaultRules()
	r.Replace = ReplaceRule{On: true, Find: "x", With: "  "}
	if got := newName(t, r, "x이름x.txt", 0); got != "이름.txt" {
		t.Errorf("trim: got %q", got)
	}
	bad := map[string]string{
		"a:b.txt":   ":",
		"a?b.txt":   "?",
		"CON.txt":   "예약",
		"nul":       "예약",
		".txt":      "",
		"":          "비어",
		"ok.txt":    "",
		"com1.data": "예약",
	}
	for name, want := range bad {
		got := ValidateName(name)
		if want == "" && name == "ok.txt" {
			if got != "" {
				t.Errorf("ValidateName(%q) = %q, want ok", name, got)
			}
			continue
		}
		if want != "" && !strings.Contains(got, want) {
			t.Errorf("ValidateName(%q) = %q, want containing %q", name, got, want)
		}
	}
	long := strings.Repeat("가", 256) + ".txt"
	if !strings.Contains(ValidateName(long), "길어요") {
		t.Error("long name not rejected")
	}
}

func TestNatural(t *testing.T) {
	names := []string{"사진10.jpg", "사진2.jpg", "사진1.jpg", "Abc.txt", "abd.txt", "사진02.jpg", "_x", "가방"}
	sort.Slice(names, func(i, j int) bool { return NaturalLess(names[i], names[j]) })
	want := []string{"_x", "Abc.txt", "abd.txt", "가방", "사진1.jpg", "사진2.jpg", "사진02.jpg", "사진10.jpg"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("got  %v\nwant %v", names, want)
	}
}

func lister(m map[string][]string) DirLister {
	return func(dir string) ([]string, error) { return m[dir], nil }
}

func TestPreviewNumberingAndConflicts(t *testing.T) {
	items := []*Item{item("c.jpg"), item("a.jpg"), item("b.jpg"), item("skip.jpg")}
	items[3].Checked = false
	r := DefaultRules()
	r.Text = TextRule{On: true, SetName: true, Name: "사진"}
	r.Number.On = true
	ps, sum, err := BuildPreview(items, r, t0, lister(map[string][]string{"/photos": {"c.jpg", "a.jpg", "b.jpg", "skip.jpg"}}))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{ps[0].NewName, ps[1].NewName, ps[2].NewName, ps[3].NewName}
	want := []string{"사진_001.jpg", "사진_002.jpg", "사진_003.jpg", "skip.jpg"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("numbering follows list order and skips unchecked: got %v", got)
	}
	if sum.Changed != 3 || sum.Skipped != 1 {
		t.Errorf("summary %+v", sum)
	}

	// Without a number every file wants the same name → all conflict.
	r.Number.On = false
	ps, sum, _ = BuildPreview(items, r, t0, nil)
	if sum.Conflict != 3 {
		t.Errorf("duplicates should conflict, summary %+v %+v", sum, ps)
	}

	// A file outside the list already has the target name.
	items = []*Item{item("a.jpg")}
	r = DefaultRules()
	r.Replace = ReplaceRule{On: true, Find: "a", With: "b"}
	ps, _, _ = BuildPreview(items, r, t0, lister(map[string][]string{"/photos": {"a.jpg", "B.JPG"}}))
	if ps[0].Status != StatusConflict {
		t.Errorf("existing file (other case) must conflict: %+v", ps[0])
	}

	// Swapping names inside the list is fine.
	items = []*Item{item("a.jpg"), item("b.jpg")}
	r.Replace = ReplaceRule{On: true, Regex: true, Find: "^a$|^b$"}
	c, _ := r.Compile(t0)
	_ = c
	// emulate a swap with two explicit previews via a rule: a→b, b→a
	r.Replace = ReplaceRule{}
	r.Text = TextRule{}
	swap := func(name string) string {
		if name == "a.jpg" {
			return "b.jpg"
		}
		return "a.jpg"
	}
	out := []Preview{{NewName: swap("a.jpg"), Status: StatusChanged}, {NewName: swap("b.jpg"), Status: StatusChanged}}
	markConflicts(items, out, lister(map[string][]string{"/photos": {"a.jpg", "b.jpg"}}))
	if out[0].Status != StatusChanged || out[1].Status != StatusChanged {
		t.Errorf("swap should be allowed: %+v", out)
	}

	// Case-only change of the same file is fine.
	items = []*Item{item("photo.JPG")}
	r = DefaultRules()
	r.Extra = ExtraRule{On: true, Ext: ExtLower}
	ps, _, _ = BuildPreview(items, r, t0, lister(map[string][]string{"/photos": {"photo.JPG"}}))
	if ps[0].Status != StatusChanged || ps[0].NewName != "photo.jpg" {
		t.Errorf("case-only change: %+v", ps[0])
	}

	// Cascade: b cannot move (its target exists outside the list), so a,
	// which wants b's current name, must conflict too.
	items = []*Item{item("a.txt"), item("b.txt")}
	out = []Preview{{NewName: "b.txt", Status: StatusChanged}, {NewName: "x.txt", Status: StatusChanged}}
	markConflicts(items, out, lister(map[string][]string{"/photos": {"a.txt", "b.txt", "x.txt"}}))
	if out[0].Status != StatusConflict || out[1].Status != StatusConflict {
		t.Errorf("cascade: %+v", out)
	}
}

func TestPreviewPerFolderAndTaken(t *testing.T) {
	a1, a2 := item("1.jpg"), item("2.jpg")
	b1 := &Item{Dir: "/other", Name: "1.jpg", Modified: t0, Checked: true}
	r := DefaultRules()
	r.Number = NumberRule{On: true, Start: 1, Step: 1, Digits: 2, Pos: Before, Sep: "-", PerFolder: true}
	ps, _, _ := BuildPreview([]*Item{a1, b1, a2}, r, t0, nil)
	if ps[0].NewName != "01-1.jpg" || ps[1].NewName != "01-1.jpg" || ps[2].NewName != "02-2.jpg" {
		t.Errorf("per folder: %v %v %v", ps[0].NewName, ps[1].NewName, ps[2].NewName)
	}

	taken := item("IMG.jpg")
	taken.Taken = time.Date(2019, 8, 15, 10, 0, 0, 0, time.Local)
	none := item("doc.jpg")
	r = DefaultRules()
	r.Date = DateRule{On: true, Source: DateTaken, Format: "YYYY-MM-DD", Pos: Before, Sep: "_"}
	ps, _, _ = BuildPreview([]*Item{taken, none}, r, t0, nil)
	if ps[0].NewName != "2019-08-15_IMG.jpg" || ps[0].Note != "" {
		t.Errorf("taken: %+v", ps[0])
	}
	if ps[1].NewName != "2024-05-01_doc.jpg" || ps[1].Note == "" {
		t.Errorf("fallback note: %+v", ps[1])
	}
}

// ---------- real files ----------

func touch(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestExecutorChainsSwapsAndUndo(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(t.TempDir(), "journal.json")
	for i, n := range []string{"1.txt", "2.txt", "3.txt", "a.txt", "b.txt", "plain.txt"} {
		touch(t, dir, n, n+string(rune('0'+i)))
	}
	ops := []Op{
		{dir, "1.txt", "2.txt"}, {dir, "2.txt", "3.txt"}, {dir, "3.txt", "4.txt"}, // shift
		{dir, "a.txt", "b.txt"}, {dir, "b.txt", "a.txt"}, // swap
		{dir, "plain.txt", "평범.txt"},
	}
	var lastDone, lastTotal int
	res := (&Executor{JournalPath: journal}).Run(ops, func(d, tot int) { lastDone, lastTotal = d, tot })
	for _, r := range res {
		if r.Err != nil {
			t.Fatalf("%v: %v", r.Op, r.Err)
		}
	}
	if lastDone != lastTotal || lastTotal == 0 {
		t.Errorf("progress %d/%d", lastDone, lastTotal)
	}
	if read(t, dir, "2.txt") != "1.txt0" || read(t, dir, "3.txt") != "2.txt1" || read(t, dir, "4.txt") != "3.txt2" {
		t.Error("shift chain content mismatch")
	}
	if read(t, dir, "b.txt") != "a.txt3" || read(t, dir, "a.txt") != "b.txt4" {
		t.Error("swap content mismatch")
	}
	if read(t, dir, "평범.txt") != "plain.txt5" {
		t.Error("plain rename failed")
	}
	if _, err := os.Stat(journal); err == nil {
		t.Error("journal should be removed after a clean run")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".namesmith-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left: %v", leftovers)
	}

	// Undo restores everything.
	res = (&Executor{}).Run(Invert(res), nil)
	for _, r := range res {
		if r.Err != nil {
			t.Fatalf("undo %v: %v", r.Op, r.Err)
		}
	}
	for i, n := range []string{"1.txt", "2.txt", "3.txt", "a.txt", "b.txt", "plain.txt"} {
		if got := read(t, dir, n); got != n+string(rune('0'+i)) {
			t.Errorf("after undo %s = %q", n, got)
		}
	}
}

func TestExecutorNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "old.txt", "old")
	touch(t, dir, "taken.txt", "precious")
	res := (&Executor{}).Run([]Op{{dir, "old.txt", "taken.txt"}}, nil)
	if res[0].Err == nil {
		t.Fatal("expected an error instead of overwriting")
	}
	if read(t, dir, "taken.txt") != "precious" || read(t, dir, "old.txt") != "old" {
		t.Fatal("files changed after a refused rename")
	}
	if !strings.Contains(DescribeError(res[0].Err), "이미 있어요") {
		t.Errorf("message: %s", DescribeError(res[0].Err))
	}

	// A swap where one target is blocked: the other file must be restored.
	dir2 := t.TempDir()
	touch(t, dir2, "a", "A")
	touch(t, dir2, "b", "B")
	failing := func(from, to string) error {
		if filepath.Base(to) == "a" && strings.Contains(filepath.Base(from), ".namesmith-") {
			return os.ErrPermission
		}
		return MoveNoReplace(from, to)
	}
	res = (&Executor{Move: failing}).Run([]Op{{dir2, "a", "b"}, {dir2, "b", "a"}}, nil)
	if res[0].Err != nil || res[1].Err == nil {
		t.Fatalf("expected a→b ok and b→a failed: %v / %v", res[0].Err, res[1].Err)
	}
	if read(t, dir2, "b") != "A" {
		t.Error("a→b should be done")
	}
	if got := read(t, dir2, "b"); got == "B" {
		t.Error("b content lost")
	}
	tmps, _ := filepath.Glob(filepath.Join(dir2, ".namesmith-*"))
	// b could not go to "a" and could not go back to "b" (now taken by A),
	// so it stays under a temporary name and the error says so.
	if len(tmps) != 1 || !strings.Contains(res[1].Err.Error(), "임시 이름") {
		t.Errorf("temp handling: %v %v", tmps, res[1].Err)
	}
}

func TestRecover(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(t.TempDir(), "j.json")
	touch(t, dir, ".namesmith-abc.tmp", "data")
	if err := writeJournal(journal, []journalEntry{{Dir: dir, From: "x.txt", Tmp: ".namesmith-abc.tmp", To: "y.txt"}}); err != nil {
		t.Fatal(err)
	}
	fixed, problems := Recover(journal, nil)
	if fixed != 1 || len(problems) != 0 || read(t, dir, "y.txt") != "data" {
		t.Fatalf("recover: fixed=%d problems=%v", fixed, problems)
	}
	if _, err := os.Stat(journal); err == nil {
		t.Error("journal should be deleted")
	}
}

func TestCollect(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "b10.txt", "")
	touch(t, root, "b2.txt", "")
	touch(t, root, ".hidden", "")
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	touch(t, filepath.Join(root, "sub"), "inner.txt", "")
	known := map[string]bool{}

	res := Collect([]string{root}, false, known, 0)
	if len(res.Items) != 2 || res.Items[0].Name != "b2.txt" || res.Items[1].Name != "b10.txt" || res.Hidden != 1 {
		t.Fatalf("flat collect: %+v", res)
	}
	res = Collect([]string{root}, true, known, 0)
	if len(res.Items) != 1 || res.Items[0].Name != "inner.txt" || res.Duplicate != 2 {
		t.Fatalf("recursive collect with known: items=%d dup=%d", len(res.Items), res.Duplicate)
	}
	res = Collect([]string{filepath.Join(root, "missing")}, false, known, 0)
	if res.Failed != 1 {
		t.Errorf("missing path should count as failed")
	}
	res = Collect([]string{root}, true, map[string]bool{}, 2)
	if len(res.Items) != 2 || !res.Truncated {
		t.Errorf("room limit: %d %v", len(res.Items), res.Truncated)
	}
}

// The examples promised in README.md.
func TestReadmeExamples(t *testing.T) {
	r := DefaultRules()
	r.Replace = ReplaceRule{On: true, Regex: true, Find: `강의 (\d)\b`, With: "강의 0$1"}
	for in, want := range map[string]string{"강의 1.mp4": "강의 01.mp4", "강의 10.mp4": "강의 10.mp4", "강의 9.mp4": "강의 09.mp4"} {
		if got := newName(t, r, in, 0); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
	r = DefaultRules()
	r.Text = TextRule{On: true, SetName: true, Name: "계약서"}
	r.Number = NumberRule{On: true, Start: 1, Step: 1, Digits: 2, Pos: After, Sep: "_"}
	if got := newName(t, r, "scan0001.pdf", 1); got != "계약서_01.pdf" {
		t.Errorf("scan: %s", got)
	}
	r = DefaultRules()
	r.Text = TextRule{On: true, Prefix: "[회사] "}
	if got := newName(t, r, "견적서.pdf", 0); got != "[회사] 견적서.pdf" {
		t.Errorf("prefix: %s", got)
	}
}

//go:build windows

// Package ui is the Windows interface of NameSmith, built with walk.
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/tailscale/walk"
	"github.com/tailscale/win"
	"golang.org/x/sys/windows"

	"github.com/game-addicted-man/NameSmith/internal/engine"
	"github.com/game-addicted-man/NameSmith/internal/meta"
)

// App holds the window and everything shown in it.
type App struct {
	app   *walk.Application
	mw    *walk.MainWindow
	tv    *walk.TableView
	model *fileModel

	emptyHint, bottomBar                   *walk.Composite
	listCount, sortInfo, status            *walk.Label
	progress                               *walk.ProgressBar
	btnRename, btnUndo                     *walk.PushButton
	btnAddFiles, btnAddFolder              *walk.PushButton
	btnUp, btnDown, btnRemove, btnClear    *walk.PushButton
	cbRecursive                            *walk.CheckBox
	rulesPanel                             *walk.ScrollView
	btnRulesOff, btnRulesReset             *walk.PushButton

	// 1. 찾아 바꾸기
	cbReplace, cbMatchCase, cbRegex, cbIncludeExt *walk.CheckBox
	leFind, leWith                                *walk.LineEdit
	// 2. 새 이름 · 글자 추가
	cbText, cbSetName             *walk.CheckBox
	leNewName, lePrefix, leSuffix *walk.LineEdit
	// 3. 날짜 넣기
	cbDate                       *walk.CheckBox
	cmbDateSource, cmbDateFormat *walk.ComboBox
	lblDateExample               *walk.Label
	rbDateBefore, rbDateAfter    *walk.RadioButton
	leDateSep                    *walk.LineEdit
	// 4. 번호 붙이기
	cbNumber                  *walk.CheckBox
	neStart, neStep, neDigits *walk.NumberEdit
	lblNumExample             *walk.Label
	rbNumBefore, rbNumAfter   *walk.RadioButton
	leNumSep                  *walk.LineEdit
	cbPerFolder               *walk.CheckBox
	// 5. 기타
	cbExtra         *walk.CheckBox
	cmbCase, cmbExt *walk.ComboBox
	leNewExt        *walk.LineEdit
	cbHangul        *walk.CheckBox

	settings     Settings
	loading      bool // widgets are being filled from code: ignore their events
	busy         bool
	ruleErr      string
	summary      engine.Summary
	known        map[string]bool     // path keys already in the list
	dirCache     map[string][]string // folder listings for conflict checks
	undo         [][]engine.OpResult
	takenPending int
	timerMu      sync.Mutex
	refreshTimer *time.Timer
	flash        string // one-off message shown until the next change
	flashGood    bool
}

// Run starts NameSmith with optional files/folders to open. It returns the
// process exit code.
func Run(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			report := fmt.Sprintf("%v\n\n%s", r, debug.Stack())
			path := filepath.Join(dataDir(), "crash.log")
			_ = os.MkdirAll(dataDir(), 0o755)
			_ = os.WriteFile(path, []byte(report), 0o644)
			msg := "죄송해요, 예상하지 못한 오류로 NameSmith를 닫아야 해요.\n\n" +
				"오류 내용을 아래 파일에 저장했어요:\n" + path + "\n\n" +
				"GitHub(" + issuesURL + ")에 알려 주시면 고칠게요."
			showFatal(msg)
			code = 2
		}
	}()

	wapp, err := walk.InitApp()
	if err != nil {
		showFatal("화면을 준비하지 못했어요: " + err.Error())
		return 1
	}
	wapp.SetOrganizationName(appName)
	wapp.SetProductName(appName)

	a := &App{
		app:      wapp,
		model:    newFileModel(),
		known:    map[string]bool{},
		dirCache: map[string][]string{},
		settings: loadSettings(),
	}
	a.model.onChange = a.changed

	// Finish a rename that was interrupted last time (power cut, crash...).
	fixed, problems := recoverJournals()

	a.loading = true
	if err := a.build(); err != nil {
		showFatal("창을 만들지 못했어요: " + err.Error())
		return 1
	}
	a.applySettings()
	a.loading = false

	// walk always creates a tool bar and a menu bar; we use neither.
	win.SetMenu(a.mw.Handle(), 0)

	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = a.mw.SetIcon(icon)
	}
	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if a.busy {
			*canceled = true
			return
		}
		a.saveSettings()
	})

	a.placeWindow()
	// Hide these only now: walk ignores SetVisible(false) while the window
	// itself is still hidden.
	if tb := a.mw.ToolBar(); tb != nil {
		win.ShowWindow(tb.Handle(), win.SW_HIDE)
	}
	win.ShowWindow(a.progress.Handle(), win.SW_HIDE)
	a.fixListImages()
	a.refresh()

	if fixed > 0 || len(problems) > 0 {
		msg := fmt.Sprintf("지난번 이름 바꾸기가 중간에 멈춰서, 파일 %d개를 마저 정리했어요.", fixed)
		if len(problems) > 0 {
			msg += "\n\n다음 파일은 임시 이름으로 남아 있어요. 직접 이름을 바꿔 주세요:\n" + strings.Join(limitLines(problems, 10), "\n")
		}
		walk.MsgBox(a.mw, appName, msg, walk.MsgBoxIconInformation)
	}
	if len(args) > 0 {
		a.addPaths(args)
	}
	return wapp.Run()
}

// fixListImages keeps file icons in the first column only (walk would
// repeat the first icon in every column).
func (a *App) fixListImages() {
	findEx := windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowExW")
	class, _ := syscall.UTF16PtrFromString("SysListView32")
	var child uintptr
	for i := 0; i < 4; i++ {
		child, _, _ = findEx.Call(uintptr(a.tv.Handle()), child, uintptr(unsafe.Pointer(class)), 0)
		if child == 0 {
			break
		}
		win.SendMessage(win.HWND(child), win.LVM_SETEXTENDEDLISTVIEWSTYLE, win.LVS_EX_SUBITEMIMAGES, 0)
	}
}

func showFatal(msg string) {
	t, _ := windows.UTF16PtrFromString(msg)
	c, _ := windows.UTF16PtrFromString(appName)
	windows.MessageBox(0, t, c, windows.MB_ICONERROR|windows.MB_OK)
}

// placeWindow restores the saved size/position (if it is still on a screen),
// or picks a size that fits the screen, and shows the window.
func (a *App) placeWindow() {
	hwnd := a.mw.Handle()
	placed := false
	if b := a.settings.Bounds; len(b) == 4 && b[2] >= 600 && b[3] >= 400 {
		_ = a.mw.SetBounds(walk.Rectangle{X: b[0], Y: b[1], Width: b[2], Height: b[3]})
		placed = win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONULL) != 0
	}
	if !placed {
		mon := win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONEAREST)
		var mi win.MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if win.GetMonitorInfo(mon, &mi) {
			work := mi.RcWork
			ww, wh := int(work.Right-work.Left), int(work.Bottom-work.Top)
			scale := float64(win.GetDpiForWindow(hwnd)) / 96
			if scale <= 0 {
				scale = 1
			}
			w := minInt(int(1240*scale), ww*94/100)
			h := minInt(int(800*scale), wh*92/100)
			_ = a.mw.SetBoundsPixels(walk.Rectangle{
				X: int(work.Left) + (ww-w)/2, Y: int(work.Top) + (wh-h)/2, Width: w, Height: h,
			})
		}
	}
	if a.settings.Maximized {
		win.ShowWindow(hwnd, win.SW_SHOWMAXIMIZED)
	} else {
		a.mw.Show()
	}
	win.SetForegroundWindow(hwnd)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *App) saveSettings() {
	s := a.settings
	s.Rules = a.readRules()
	s.Recursive = a.cbRecursive.Checked()
	s.Maximized = win.IsZoomed(a.mw.Handle())
	if !s.Maximized && !win.IsIconic(a.mw.Handle()) {
		bb := a.mw.Bounds()
		s.Bounds = []int{bb.X, bb.Y, bb.Width, bb.Height}
	}
	_ = saveSettings(s)
}

// ---------- preview ----------

// changed is called whenever the user edits a rule or the list.
func (a *App) changed() {
	if a.loading {
		return
	}
	a.flash = ""
	a.scheduleRefresh()
}

// touch switches a rule section on when one of its fields is edited.
func (a *App) touch(section *walk.CheckBox) {
	if a.loading {
		return
	}
	if section != nil && !section.Checked() {
		section.SetChecked(true) // fires changed()
	}
	a.changed()
}

func (a *App) scheduleRefresh() {
	a.timerMu.Lock()
	defer a.timerMu.Unlock()
	if a.refreshTimer != nil {
		a.refreshTimer.Stop()
	}
	a.refreshTimer = time.AfterFunc(120*time.Millisecond, func() {
		a.app.Synchronize(a.refresh)
	})
}

func (a *App) listDir(dir string) ([]string, error) {
	k := engine.DirKey(dir)
	if names, ok := a.dirCache[k]; ok {
		return names, nil
	}
	names, err := engine.ListDir(dir)
	if err != nil {
		return nil, err
	}
	a.dirCache[k] = names
	return names, nil
}

// refresh recomputes every new name and updates the screen.
func (a *App) refresh() {
	if a.mw == nil || a.busy {
		return
	}
	rules := a.readRules()
	a.updateExamples(rules)

	items := a.model.items
	c, err := rules.Compile(time.Now())
	if err != nil {
		a.ruleErr = err.Error()
		prev := make([]engine.Preview, len(items))
		for i, it := range items {
			prev[i] = engine.Preview{NewName: it.Name, Status: engine.StatusUnchanged}
			if !it.Checked {
				prev[i].Status = engine.StatusSkipped
			}
		}
		a.model.prev = prev
	} else {
		a.ruleErr = ""
		a.model.prev = c.Preview(items, a.listDir)
	}
	a.summary = engine.Summarize(a.model.prev)
	if n := len(items); n > 0 {
		a.model.PublishRowsChanged(0, n-1)
	}
	a.updateChrome(rules)
}

func (a *App) needsTaken(r engine.Rules) bool {
	return r.Date.On && r.Date.Source == engine.DateTaken
}

// updateChrome refreshes labels and buttons around the list.
func (a *App) updateChrome(rules engine.Rules) {
	n := len(a.model.items)
	a.emptyHint.SetVisible(n == 0)
	a.tv.SetVisible(n > 0)
	if n == 0 {
		a.listCount.SetText("")
	} else {
		a.listCount.SetText(fmt.Sprintf("%d개", n))
	}
	a.sortInfo.SetText(a.model.sortLabel())

	waitTaken := a.takenPending > 0 && a.needsTaken(rules)
	text := summaryText(a.summary, rules.AnyOn())
	color := colText
	switch {
	case a.busy:
		text, color = "이름을 바꾸는 중이에요…", colText
	case a.ruleErr != "":
		text, color = a.ruleErr, colRed
	case waitTaken:
		text = fmt.Sprintf("사진·동영상의 찍은 날짜를 읽는 중이에요… (남은 파일 %d개)", a.takenPending)
	case a.flash != "":
		text = a.flash
		if a.flashGood {
			color = colGreen
		}
	case a.summary.Conflict+a.summary.Invalid > 0:
		color = colRed
	}
	if a.status.Text() != text {
		a.status.SetText(text)
		a.status.Invalidate()
		a.bottomBar.Invalidate()
	}
	a.status.SetTextColor(color)

	canRename := !a.busy && a.ruleErr == "" && !waitTaken && a.summary.Changed > 0
	a.btnRename.SetEnabled(canRename)
	if a.summary.Changed > 0 {
		a.btnRename.SetText(fmt.Sprintf("이름 바꾸기 (%d)", a.summary.Changed))
	} else {
		a.btnRename.SetText("이름 바꾸기")
	}
	a.btnUndo.SetEnabled(!a.busy && len(a.undo) > 0)
	if len(a.undo) > 0 {
		a.btnUndo.SetToolTipText(fmt.Sprintf("마지막으로 바꾼 %d개를 원래 이름으로 돌려요 (Ctrl+Z)", len(a.undo[len(a.undo)-1])))
	} else {
		a.btnUndo.SetToolTipText("이름을 바꾼 뒤에 쓸 수 있어요")
	}

	hasSel := len(a.tv.SelectedIndexes()) > 0
	for _, b := range []*walk.PushButton{a.btnUp, a.btnDown, a.btnRemove} {
		b.SetEnabled(!a.busy && hasSel)
	}
	a.btnClear.SetEnabled(!a.busy && n > 0)
	a.btnAddFiles.SetEnabled(!a.busy)
	a.btnAddFolder.SetEnabled(!a.busy)
}

// updateExamples shows "예: …" next to the date and number options.
func (a *App) updateExamples(r engine.Rules) {
	f := r.Date.Format
	if strings.TrimSpace(f) == "" {
		f = "YYYY-MM-DD"
	}
	ex := engine.FormatDate(time.Now(), f)
	if v := engine.ValidateName(ex + ".x"); v != "" && !strings.Contains(v, "비어") {
		a.lblDateExample.SetText("예: " + ex + "  ← 파일 이름에 쓸 수 없는 글자가 있어요")
		a.lblDateExample.SetTextColor(colRed)
	} else {
		a.lblDateExample.SetText("예: " + ex + " (오늘)")
		a.lblDateExample.SetTextColor(colMuted)
	}
	n := r.Number
	if n.Step == 0 {
		n.Step = 1
	}
	if n.Digits < 1 {
		n.Digits = 1
	}
	a.lblNumExample.SetText(fmt.Sprintf("예: %s, %s, %s …",
		engine.FormatNumber(n.Start, n.Digits),
		engine.FormatNumber(n.Start+n.Step, n.Digits),
		engine.FormatNumber(n.Start+2*n.Step, n.Digits)))
}

// ---------- reading and writing rule widgets ----------

func (a *App) readRules() engine.Rules {
	r := engine.DefaultRules()
	r.Replace = engine.ReplaceRule{
		On:         a.cbReplace.Checked(),
		Find:       a.leFind.Text(),
		With:       a.leWith.Text(),
		MatchCase:  a.cbMatchCase.Checked(),
		Regex:      a.cbRegex.Checked(),
		IncludeExt: a.cbIncludeExt.Checked(),
	}
	r.Text = engine.TextRule{
		On:      a.cbText.Checked(),
		SetName: a.cbSetName.Checked(),
		Name:    a.leNewName.Text(),
		Prefix:  a.lePrefix.Text(),
		Suffix:  a.leSuffix.Text(),
	}
	r.Date = engine.DateRule{
		On:     a.cbDate.Checked(),
		Source: engine.DateSource(clampIndex(a.cmbDateSource.CurrentIndex(), 4)),
		Format: a.cmbDateFormat.Text(),
		Pos:    engine.Before,
		Sep:    a.leDateSep.Text(),
	}
	if a.rbDateAfter.Checked() {
		r.Date.Pos = engine.After
	}
	r.Number = engine.NumberRule{
		On:        a.cbNumber.Checked(),
		Start:     int(a.neStart.Value()),
		Step:      int(a.neStep.Value()),
		Digits:    int(a.neDigits.Value()),
		Pos:       engine.After,
		Sep:       a.leNumSep.Text(),
		PerFolder: a.cbPerFolder.Checked(),
	}
	if a.rbNumBefore.Checked() {
		r.Number.Pos = engine.Before
	}
	r.Extra = engine.ExtraRule{
		On:            a.cbExtra.Checked(),
		Case:          engine.CaseMode(clampIndex(a.cmbCase.CurrentIndex(), 4)),
		Ext:           engine.ExtMode(clampIndex(a.cmbExt.CurrentIndex(), 5)),
		NewExt:        a.leNewExt.Text(),
		ComposeHangul: a.cbHangul.Checked(),
	}
	return r
}

func clampIndex(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

func (a *App) applyRules(r engine.Rules) {
	was := a.loading
	a.loading = true
	defer func() { a.loading = was }()

	a.cbReplace.SetChecked(r.Replace.On)
	a.leFind.SetText(r.Replace.Find)
	a.leWith.SetText(r.Replace.With)
	a.cbMatchCase.SetChecked(r.Replace.MatchCase)
	a.cbRegex.SetChecked(r.Replace.Regex)
	a.cbIncludeExt.SetChecked(r.Replace.IncludeExt)

	a.cbText.SetChecked(r.Text.On)
	a.cbSetName.SetChecked(r.Text.SetName)
	a.leNewName.SetText(r.Text.Name)
	a.lePrefix.SetText(r.Text.Prefix)
	a.leSuffix.SetText(r.Text.Suffix)

	a.cbDate.SetChecked(r.Date.On)
	a.cmbDateSource.SetCurrentIndex(clampIndex(int(r.Date.Source), 4))
	a.cmbDateFormat.SetText(r.Date.Format)
	a.rbDateBefore.SetChecked(r.Date.Pos == engine.Before)
	a.rbDateAfter.SetChecked(r.Date.Pos == engine.After)
	a.leDateSep.SetText(r.Date.Sep)

	a.cbNumber.SetChecked(r.Number.On)
	_ = a.neStart.SetValue(float64(r.Number.Start))
	_ = a.neStep.SetValue(float64(r.Number.Step))
	_ = a.neDigits.SetValue(float64(r.Number.Digits))
	a.rbNumBefore.SetChecked(r.Number.Pos == engine.Before)
	a.rbNumAfter.SetChecked(r.Number.Pos == engine.After)
	a.leNumSep.SetText(r.Number.Sep)
	a.cbPerFolder.SetChecked(r.Number.PerFolder)

	a.cbExtra.SetChecked(r.Extra.On)
	a.cmbCase.SetCurrentIndex(clampIndex(int(r.Extra.Case), 4))
	a.cmbExt.SetCurrentIndex(clampIndex(int(r.Extra.Ext), 5))
	a.leNewExt.SetText(r.Extra.NewExt)
	a.cbHangul.SetChecked(r.Extra.ComposeHangul)
}

func (a *App) applySettings() {
	a.applyRules(a.settings.Rules)
	a.cbRecursive.SetChecked(a.settings.Recursive)
}

// rulesOff switches every section off but keeps what was typed.
func (a *App) rulesOff() {
	was := a.loading
	a.loading = true
	for _, cb := range []*walk.CheckBox{a.cbReplace, a.cbText, a.cbDate, a.cbNumber, a.cbExtra} {
		cb.SetChecked(false)
	}
	a.loading = was
}

// ---------- adding and removing files ----------

func (a *App) addFiles() {
	paths, err := pickPaths(windows.HWND(a.mw.Handle()), false, "이름을 바꿀 파일 고르기 (여러 개 선택 가능)", "추가", a.settings.LastDir)
	if err != nil {
		walk.MsgBox(a.mw, appName, err.Error(), walk.MsgBoxIconWarning)
		return
	}
	a.addPaths(paths)
}

func (a *App) addFolder() {
	paths, err := pickPaths(windows.HWND(a.mw.Handle()), true, "파일이 들어 있는 폴더 고르기 (여러 개 선택 가능)", "폴더 추가", a.settings.LastDir)
	if err != nil {
		walk.MsgBox(a.mw, appName, err.Error(), walk.MsgBoxIconWarning)
		return
	}
	a.addPaths(paths)
}

func (a *App) addPaths(paths []string) {
	if a.busy || len(paths) == 0 {
		return
	}
	room := engine.MaxItems - len(a.model.items)
	if room <= 0 {
		walk.MsgBox(a.mw, appName, fmt.Sprintf("목록에는 최대 %d개까지 넣을 수 있어요.", engine.MaxItems), walk.MsgBoxIconWarning)
		return
	}
	a.mw.SetCursor(walk.CursorWait())
	res := engine.Collect(paths, a.cbRecursive.Checked(), a.known, room)
	a.mw.SetCursor(nil)

	// A folder that only holds folders gives nothing without "하위 폴더 포함".
	if len(res.Items) == 0 && res.Duplicate == 0 && !a.cbRecursive.Checked() && hasSubfolders(paths) {
		q := "고른 폴더에는 파일이 없고 폴더만 들어 있어요.\n안쪽 폴더의 파일까지 모두 넣을까요?"
		if walk.MsgBox(a.mw, appName, q, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == win.IDYES {
			a.cbRecursive.SetChecked(true)
			a.mw.SetCursor(walk.CursorWait())
			res = engine.Collect(paths, true, a.known, room)
			a.mw.SetCursor(nil)
		}
	}

	if len(res.Items) > 0 {
		a.model.items = append(a.model.items, res.Items...)
		a.model.sortItems()
		a.model.prev = nil
		a.dirCache = map[string][]string{}
		a.model.PublishRowsReset()
		a.loadTaken(res.Items)

		first := paths[0]
		if st, err := os.Stat(first); err == nil && st.IsDir() {
			a.settings.LastDir = first
		} else {
			a.settings.LastDir = filepath.Dir(first)
		}
	}

	var notes []string
	if res.Duplicate > 0 {
		notes = append(notes, fmt.Sprintf("이미 있는 %d개", res.Duplicate))
	}
	if res.Hidden > 0 {
		notes = append(notes, fmt.Sprintf("숨김·시스템 파일 %d개", res.Hidden))
	}
	if res.Failed > 0 {
		notes = append(notes, fmt.Sprintf("읽을 수 없는 %d개", res.Failed))
	}
	msg := fmt.Sprintf("파일 %d개를 추가했어요.", len(res.Items))
	if len(res.Items) == 0 {
		msg = "추가할 새 파일이 없어요."
		if !a.cbRecursive.Checked() {
			msg += " (폴더 안의 폴더까지 넣으려면 '하위 폴더 포함'을 켜세요)"
		}
	}
	if len(notes) > 0 {
		msg += " " + strings.Join(notes, ", ") + "는 뺐어요."
	}
	a.flash, a.flashGood = msg, len(res.Items) > 0
	a.refresh()
	if res.Truncated {
		walk.MsgBox(a.mw, appName, fmt.Sprintf("파일이 너무 많아서 %d개까지만 넣었어요.", engine.MaxItems), walk.MsgBoxIconWarning)
	}
}

func hasSubfolders(paths []string) bool {
	for _, p := range paths {
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				return true
			}
		}
	}
	return false
}

// loadTaken reads photo/video dates in the background.
func (a *App) loadTaken(items []*engine.Item) {
	type job struct {
		it   *engine.Item
		path string
	}
	var jobs []job
	for _, it := range items {
		if it.TakenLoaded {
			continue
		}
		if !meta.IsMedia(it.Name) {
			it.TakenLoaded = true
			continue
		}
		jobs = append(jobs, job{it, it.Path()})
	}
	if len(jobs) == 0 {
		return
	}
	a.takenPending += len(jobs)

	type result struct {
		it *engine.Item
		t  time.Time
	}
	go func() {
		in := make(chan job)
		out := make(chan result, 64)
		var wg sync.WaitGroup
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range in {
					t, _ := meta.DateTaken(j.path)
					out <- result{j.it, t}
				}
			}()
		}
		go func() {
			for _, j := range jobs {
				in <- j
			}
			close(in)
			wg.Wait()
			close(out)
		}()
		var batch []result
		last := time.Now()
		flush := func() {
			if len(batch) == 0 {
				return
			}
			b := batch
			batch = nil
			a.app.Synchronize(func() {
				for _, r := range b {
					r.it.Taken = r.t
					r.it.TakenLoaded = true
				}
				a.takenPending -= len(b)
				if a.takenPending < 0 {
					a.takenPending = 0
				}
				if !a.busy {
					a.refresh()
				}
			})
		}
		for r := range out {
			batch = append(batch, r)
			if len(batch) >= 200 || time.Since(last) > 400*time.Millisecond {
				flush()
				last = time.Now()
			}
		}
		flush()
	}()
}

func (a *App) removeSelected() {
	if a.busy {
		return
	}
	sel := a.tv.SelectedIndexes()
	if len(sel) == 0 {
		return
	}
	first := sel[0]
	for _, i := range sel {
		if i < len(a.model.items) {
			delete(a.known, engine.PathKey(a.model.items[i].Path()))
		}
	}
	a.model.remove(sel)
	a.model.PublishRowsReset()
	a.flash = ""
	a.refresh()
	if n := len(a.model.items); n > 0 {
		if first >= n {
			first = n - 1
		}
		_ = a.tv.SetSelectedIndexes([]int{first})
	}
}

func (a *App) clearAll() {
	if a.busy || len(a.model.items) == 0 {
		return
	}
	a.model.items = nil
	a.model.prev = nil
	a.known = map[string]bool{}
	a.dirCache = map[string][]string{}
	a.model.sortCol = -1
	a.model.PublishRowsReset()
	a.flash = ""
	a.refresh()
}

func (a *App) move(delta int) {
	if a.busy {
		return
	}
	sel := a.tv.SelectedIndexes()
	if len(sel) == 0 {
		return
	}
	newSel := a.model.moveRows(sel, delta)
	a.model.PublishRowsReset()
	a.refresh()
	_ = a.tv.SetSelectedIndexes(newSel)
	if len(newSel) > 0 {
		a.tv.EnsureItemVisible(newSel[0])
	}
	a.tv.SetFocus()
}

func (a *App) sortBy(col int, order walk.SortOrder) {
	if a.busy {
		return
	}
	a.model.sortBy(col, order)
	a.model.PublishRowsReset()
	a.refresh()
}

func (a *App) setChecked(checked bool) {
	sel := a.tv.SelectedIndexes()
	if len(sel) == 0 {
		for i := range a.model.items {
			sel = append(sel, i)
		}
	}
	for _, i := range sel {
		if i < len(a.model.items) {
			a.model.items[i].Checked = checked
		}
	}
	a.changed()
}

func (a *App) selectAll() {
	all := make([]int, len(a.model.items))
	for i := range all {
		all[i] = i
	}
	_ = a.tv.SetSelectedIndexes(all)
}

func (a *App) openSelectedFolder() {
	i := a.tv.CurrentIndex()
	if i < 0 || i >= len(a.model.items) {
		return
	}
	openInExplorer(a.model.items[i].Path(), true)
}

func (a *App) reloadFromDisk() {
	if a.busy {
		return
	}
	kept := a.model.items[:0]
	for _, it := range a.model.items {
		if engine.Refresh(it) {
			kept = append(kept, it)
		} else {
			delete(a.known, engine.PathKey(it.Path()))
		}
	}
	a.model.items = kept
	a.dirCache = map[string][]string{}
	a.model.PublishRowsReset()
	a.refresh()
}

// ---------- renaming ----------

func (a *App) rename() {
	if a.busy {
		return
	}
	a.dirCache = map[string][]string{} // look at the folders again right now
	a.refresh()
	if a.ruleErr != "" || a.summary.Changed == 0 {
		return
	}
	var ops []engine.Op
	for i, p := range a.model.prev {
		if p.Status == engine.StatusChanged {
			it := a.model.items[i]
			ops = append(ops, engine.Op{Dir: it.Dir, From: it.Name, To: p.NewName})
		}
	}
	if skip := a.summary.Conflict + a.summary.Invalid; skip > 0 {
		msg := fmt.Sprintf("이름이 겹치거나 쓸 수 없는 파일 %d개는 건너뛰고,\n나머지 %d개만 이름을 바꿀까요?", skip, len(ops))
		if walk.MsgBox(a.mw, appName, msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != win.IDYES {
			return
		}
	}
	a.runOps(ops, false)
}

func (a *App) undoLast() {
	if a.busy || len(a.undo) == 0 {
		return
	}
	batch := a.undo[len(a.undo)-1]
	a.undo = a.undo[:len(a.undo)-1]
	a.runOps(engine.Invert(batch), true)
}

func (a *App) setBusy(b bool) {
	a.busy = b
	a.rulesPanel.SetEnabled(!b)
	a.tv.SetEnabled(!b)
	a.cbRecursive.SetEnabled(!b)
	a.progress.SetVisible(b)
	if b {
		a.progress.SetValue(0)
	}
	a.bottomBar.Invalidate()
}

func (a *App) runOps(ops []engine.Op, isUndo bool) {
	if len(ops) == 0 {
		return
	}
	a.setBusy(true)
	a.progress.SetRange(0, len(ops))
	a.updateChrome(a.readRules())
	go func() {
		last := time.Time{}
		ex := &engine.Executor{JournalPath: journalPath()}
		res := ex.Run(ops, func(done, total int) {
			if done == total || time.Since(last) > 60*time.Millisecond {
				last = time.Now()
				a.app.Synchronize(func() {
					a.progress.SetRange(0, total)
					a.progress.SetValue(done)
				})
			}
		})
		a.app.Synchronize(func() { a.finishOps(res, isUndo) })
	}()
}

func (a *App) finishOps(res []engine.OpResult, isUndo bool) {
	byPath := make(map[string]*engine.Item, len(a.model.items))
	for _, it := range a.model.items {
		byPath[engine.PathKey(it.Path())] = it
	}
	var ok, failed []engine.OpResult
	for _, r := range res {
		if r.Err != nil {
			failed = append(failed, r)
			continue
		}
		ok = append(ok, r)
		oldKey := engine.PathKey(filepath.Join(r.Op.Dir, r.Op.From))
		if it := byPath[oldKey]; it != nil {
			if a.known[oldKey] {
				delete(a.known, oldKey)
			}
			it.Name = r.Op.To
			a.known[engine.PathKey(it.Path())] = true
		}
	}
	if !isUndo && len(ok) > 0 {
		a.undo = append(a.undo, ok)
		if len(a.undo) > 20 {
			a.undo = a.undo[len(a.undo)-20:]
		}
	}
	_, _ = engine.WriteLog(logDir(), res, time.Now())

	a.setBusy(false)
	a.dirCache = map[string][]string{}
	if !isUndo && len(ok) > 0 {
		a.rulesOff()
	}
	switch {
	case isUndo:
		a.flash = fmt.Sprintf("%d개 파일을 원래 이름으로 되돌렸어요.", len(ok))
	case len(ok) > 0:
		a.flash = fmt.Sprintf("완료! %d개 파일 이름을 바꿨어요. 잘못됐다면 [되돌리기]를 누르세요.", len(ok))
	default:
		a.flash = "이름을 바꾼 파일이 없어요."
	}
	a.flashGood = len(ok) > 0
	a.model.PublishRowsReset()
	a.refresh()

	if len(failed) > 0 {
		lines := make([]string, 0, len(failed))
		for _, r := range failed {
			lines = append(lines, "• "+r.Op.From+"  →  "+engine.DescribeError(r.Err))
		}
		what := "이름을 바꾸지"
		if isUndo {
			what = "되돌리지"
		}
		msg := fmt.Sprintf("%d개는 %s 못했어요:\n\n%s", len(failed), what, strings.Join(limitLines(lines, 12), "\n"))
		walk.MsgBox(a.mw, appName, msg, walk.MsgBoxIconWarning)
	}
}

func limitLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	out := append([]string{}, lines[:n]...)
	return append(out, fmt.Sprintf("… 외 %d개", len(lines)-n))
}

// ---------- links ----------

func (a *App) onLink(link *walk.LinkLabelLink) {
	switch link.Id() {
	case "help":
		openURL(helpURL)
	case "site":
		openURL(repoURL)
	case "logs":
		_ = os.MkdirAll(logDir(), 0o755)
		openInExplorer(logDir(), false)
	}
}

func openURL(u string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(u)
	win.ShellExecute(0, verb, file, nil, nil, win.SW_SHOWNORMAL)
}

// openInExplorer opens a folder, or (selectFile) the folder of a file with
// the file highlighted.
func openInExplorer(path string, selectFile bool) {
	if selectFile {
		args, _ := syscall.UTF16PtrFromString(`/select,"` + path + `"`)
		file, _ := syscall.UTF16PtrFromString("explorer.exe")
		win.ShellExecute(0, nil, file, args, nil, win.SW_SHOWNORMAL)
		return
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(path)
	win.ShellExecute(0, verb, file, nil, nil, win.SW_SHOWNORMAL)
}

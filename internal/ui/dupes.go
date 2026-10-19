//go:build windows

package ui

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tailscale/walk"
	"github.com/tailscale/win"
	"golang.org/x/sys/windows"

	"github.com/game-addicted-man/NameSmith/internal/dupes"
)

// Columns of the duplicates table.
const (
	dcName = iota
	dcGroup
	dcRelation
	dcSize
	dcPixels
	dcModified
	dcFolder
)

var colGroupBg = walk.RGB(244, 246, 250)

type dupeRow struct {
	g     *dupes.Group
	gi    int
	f     *dupes.File
	first bool
}

// dupeModel feeds the duplicates table: one row per file, groups one after
// another, the file to keep first in each group.
type dupeModel struct {
	walk.TableModelBase
	d    *dupesPage
	rows []dupeRow
}

func (m *dupeModel) RowCount() int { return len(m.rows) }

func (m *dupeModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	r := m.rows[row]
	f := r.f
	switch col {
	case dcName:
		return f.Name
	case dcGroup:
		if r.first {
			return itoa(r.gi + 1)
		}
		return ""
	case dcRelation:
		return m.d.relationText(r)
	case dcSize:
		return formatSize(f.Size)
	case dcPixels:
		if f.W > 0 {
			return fmt.Sprintf("%d × %d", f.W, f.H)
		}
		return ""
	case dcModified:
		return formatTime(f.Modified)
	case dcFolder:
		return f.Dir
	}
	return ""
}

func (m *dupeModel) Image(row int) interface{} {
	if row < 0 || row >= len(m.rows) {
		return nil
	}
	return m.rows[row].f.Path
}

func (m *dupeModel) Checked(row int) bool {
	return row >= 0 && row < len(m.rows) && m.d.checked[m.rows[row].f]
}

func (m *dupeModel) SetChecked(row int, checked bool) error {
	if row >= 0 && row < len(m.rows) {
		m.d.setChecked(m.rows[row].f, checked)
	}
	return nil
}

// dupesPage is the "중복 파일 찾기" screen.
type dupesPage struct {
	a *App

	page                                  *walk.Composite
	lbRoots                               *walk.ListBox
	btnAddRoot, btnRemoveRoot, btnScan    *walk.PushButton
	cbExact, cbSimilar, cbRecursive       *walk.CheckBox
	cmbKind, cmbLevel, cmbKeep            *walk.ComboBox
	tv                                    *walk.TableView
	model                                 *dupeModel
	emptyHint, bottomBar                  *walk.Composite
	emptyTitle, emptyText, resultInfo     *walk.Label
	btnUncheckAll                         *walk.PushButton
	iv                                    *walk.ImageView
	lblPName, lblPInfo, lblPDate          *walk.Label
	lblPFolder, lblPNote                  *walk.Label
	btnOpen, btnReveal                    *walk.PushButton
	status                                *walk.Label
	progress                              *walk.ProgressBar
	btnCancel, btnUndo, btnMove, btnTrash *walk.PushButton
	boldFont                              *walk.Font

	roots   []string
	result  *dupes.Result
	checked map[*dupes.File]bool
	cancel  context.CancelFunc
	busy    bool // scanning, moving or recycling
	scanned bool // a scan finished at least once
	cleaned int  // files cleaned up since the last scan
	flash   string
	flashOK bool

	undoMoves  []dupes.Moved
	undoGroups []*dupes.Group
	undoCheck  map[*dupes.File]bool

	previewBmp *walk.Bitmap
	previewSeq int
	progText   string
}

func newDupesPage(a *App) *dupesPage {
	d := &dupesPage{a: a, checked: map[*dupes.File]bool{}}
	d.model = &dupeModel{d: d}
	return d
}

// afterCreate finishes setting up widgets once the window exists.
func (d *dupesPage) afterCreate(s Settings) {
	d.boldFont, _ = walk.NewFont(uiFont, 9, walk.FontBold)
	d.cbExact.SetChecked(!s.DupeNoExact)
	d.cbSimilar.SetChecked(!s.DupeNoSimilar)
	d.cbRecursive.SetChecked(!s.DupeNoRecursive)
	d.cmbKind.SetCurrentIndex(clampIndex(s.DupeKind, len(dupes.KindLabels)))
	lv := s.DupeLevel
	if lv == 0 && !s.DupeLevelSet {
		lv = int(dupes.LevelNormal)
	}
	d.cmbLevel.SetCurrentIndex(clampIndex(lv, len(dupes.LevelLabels)))
	d.cmbKeep.SetCurrentIndex(clampIndex(s.DupeKeep, len(dupes.KeepLabels)))
	for _, r := range s.DupeRoots {
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			d.roots = append(d.roots, r)
		}
	}
	d.refreshRoots()
	d.setPreviewImage(nil)
	d.updateChrome()
}

func (d *dupesPage) saveTo(s *Settings) {
	s.DupeNoExact = !d.cbExact.Checked()
	s.DupeNoSimilar = !d.cbSimilar.Checked()
	s.DupeNoRecursive = !d.cbRecursive.Checked()
	s.DupeKind = d.cmbKind.CurrentIndex()
	s.DupeLevel = d.cmbLevel.CurrentIndex()
	s.DupeLevelSet = true
	s.DupeKeep = d.cmbKeep.CurrentIndex()
	s.DupeRoots = append([]string(nil), d.roots...)
}

// ---------- folders ----------

func (d *dupesPage) addRootsDialog() {
	paths, err := pickPaths(windows.HWND(d.a.mw.Handle()), true, "중복 파일을 찾을 폴더 고르기 (여러 개 선택 가능)", "폴더 추가", d.a.settings.LastDir)
	if err != nil {
		walk.MsgBox(d.a.mw, appName, err.Error(), walk.MsgBoxIconWarning)
		return
	}
	d.addRoots(paths)
}

// addRoots adds folders (a dropped file adds its folder).
func (d *dupesPage) addRoots(paths []string) {
	if d.busy {
		return
	}
	added := 0
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			p = filepath.Dir(p)
		}
		p = filepath.Clean(p)
		dup := false
		for _, r := range d.roots {
			if strings.EqualFold(r, p) {
				dup = true
				break
			}
		}
		if !dup {
			d.roots = append(d.roots, p)
			added++
		}
	}
	if added > 0 {
		d.a.settings.LastDir = d.roots[len(d.roots)-1]
	}
	d.refreshRoots()
	d.updateChrome()
}

func (d *dupesPage) removeRoot() {
	i := d.lbRoots.CurrentIndex()
	if d.busy || i < 0 || i >= len(d.roots) {
		return
	}
	d.roots = append(d.roots[:i], d.roots[i+1:]...)
	d.refreshRoots()
	if len(d.roots) > 0 {
		if i >= len(d.roots) {
			i = len(d.roots) - 1
		}
		d.lbRoots.SetCurrentIndex(i)
	}
	d.updateChrome()
}

func (d *dupesPage) refreshRoots() {
	items := append([]string(nil), d.roots...)
	if len(items) == 0 {
		items = []string{"(아직 없어요 — [폴더 추가]를 누르거나 폴더를 이 창으로 끌어다 놓으세요)"}
	}
	d.lbRoots.SetModel(items)
}

// ---------- scanning ----------

func (d *dupesPage) startScan() {
	if d.busy {
		return
	}
	if len(d.roots) == 0 {
		walk.MsgBox(d.a.mw, appName, "먼저 중복 파일을 찾을 폴더를 추가해 주세요.", walk.MsgBoxIconInformation)
		return
	}
	if !d.cbExact.Checked() && !d.cbSimilar.Checked() {
		walk.MsgBox(d.a.mw, appName, "'내용이 똑같은 파일'이나 '비슷한 사진' 중 하나는 켜 주세요.", walk.MsgBoxIconInformation)
		return
	}
	opt := dupes.Options{
		Roots:     append([]string(nil), d.roots...),
		Recursive: d.cbRecursive.Checked(),
		Exact:     d.cbExact.Checked(),
		Similar:   d.cbSimilar.Checked(),
		Kind:      dupes.Kind(clampIndex(d.cmbKind.CurrentIndex(), len(dupes.KindLabels))),
		Level:     dupes.Level(clampIndex(d.cmbLevel.CurrentIndex(), len(dupes.LevelLabels))),
		CachePath: filepath.Join(dataDir(), "imagehash.gob"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.setBusy(true)
	d.result = nil
	d.cleaned = 0
	d.checked = map[*dupes.File]bool{}
	d.undoMoves, d.undoGroups = nil, nil
	d.rebuildRows()
	d.flash = ""
	d.progText = "파일 목록을 만드는 중이에요…"
	d.progress.SetMarqueeMode(true)
	d.updateChrome()

	go func() {
		res, err := dupes.Scan(ctx, opt, func(p dupes.Progress) {
			d.a.app.Synchronize(func() { d.onProgress(p) })
		})
		d.a.app.Synchronize(func() { d.onScanDone(res, err) })
	}()
}

func (d *dupesPage) cancelScan() {
	if d.cancel != nil {
		d.cancel()
		d.progText = "멈추는 중이에요…"
		d.updateChrome()
	}
}

func (d *dupesPage) onProgress(p dupes.Progress) {
	if !d.busy || d.cancel == nil {
		return
	}
	switch p.Phase {
	case dupes.PhaseList:
		d.progText = fmt.Sprintf("파일 목록을 만드는 중이에요… %s개", groupDigits(p.Found))
		d.progress.SetMarqueeMode(true)
	default:
		label := map[dupes.Phase]string{
			dupes.PhaseCompare: "같은 크기의 파일을 비교하는 중이에요…",
			dupes.PhasePhotos:  "사진을 살펴보는 중이에요…",
			dupes.PhaseGroup:   "비슷한 사진을 묶는 중이에요…",
		}[p.Phase]
		if p.Total > 0 {
			d.progText = fmt.Sprintf("%s %s / %s", label, groupDigits(p.Done), groupDigits(p.Total))
			d.progress.SetMarqueeMode(false)
			d.progress.SetRange(0, p.Total)
			d.progress.SetValue(p.Done)
		} else {
			d.progText = label
		}
	}
	d.updateChrome()
}

func (d *dupesPage) onScanDone(res *dupes.Result, err error) {
	d.cancel = nil
	d.progress.SetMarqueeMode(false)
	d.setBusy(false)
	switch {
	case errors.Is(err, context.Canceled):
		d.flash, d.flashOK = "찾기를 멈췄어요.", false
	case err != nil:
		d.flash, d.flashOK = "찾지 못했어요: "+err.Error(), false
	default:
		d.result = res
		d.scanned = true
		d.applyKeep()
		d.flash = d.scanSummary()
		d.flashOK = len(res.Groups) > 0
	}
	d.rebuildRows()
	d.updateChrome()
	if d.tv.Visible() && d.model.RowCount() > 0 {
		d.tv.SetCurrentIndex(0)
	}
}

func (d *dupesPage) scanSummary() string {
	r := d.result
	secs := int(r.Elapsed.Round(time.Second) / time.Second)
	took := fmt.Sprintf("%d초", secs)
	if secs < 1 {
		took = "1초 안"
	}
	msg := fmt.Sprintf("파일 %s개를 살펴봤어요 (%s)", groupDigits(r.Files), took)
	if len(r.Groups) == 0 {
		msg = "중복 파일이 없어요! " + msg
	}
	var skip []string
	if r.Skipped.Cloud > 0 {
		skip = append(skip, fmt.Sprintf("내려받지 않은 클라우드 파일 %d개", r.Skipped.Cloud))
	}
	if r.Skipped.Pictures > 0 {
		skip = append(skip, fmt.Sprintf("열 수 없는 사진 %d개", r.Skipped.Pictures))
	}
	if r.Skipped.Unreadable > 0 {
		skip = append(skip, fmt.Sprintf("읽을 수 없는 파일·폴더 %d개", r.Skipped.Unreadable))
	}
	if len(skip) > 0 {
		msg += " · 건너뜀: " + strings.Join(skip, ", ")
	}
	return msg
}

// applyKeep picks the file to keep in every group (by the 남길 파일 rule)
// and checks all the others.
func (d *dupesPage) applyKeep() {
	if d.result == nil {
		return
	}
	rule := dupes.KeepRule(clampIndex(d.cmbKeep.CurrentIndex(), len(dupes.KeepLabels)))
	d.checked = map[*dupes.File]bool{}
	for _, g := range d.result.Groups {
		g.ChooseKeep(rule, len(d.roots))
		for _, f := range g.Files[1:] {
			d.checked[f] = true
		}
	}
}

func (d *dupesPage) onKeepRuleChanged() {
	if d.a.loading || d.busy || d.result == nil {
		return
	}
	d.applyKeep()
	d.flash = ""
	d.rebuildRows()
	d.updateChrome()
}

func (d *dupesPage) rebuildRows() {
	var rows []dupeRow
	if d.result != nil {
		for gi, g := range d.result.Groups {
			for i, f := range g.Files {
				rows = append(rows, dupeRow{g: g, gi: gi, f: f, first: i == 0})
			}
		}
	}
	d.model.rows = rows
	d.model.PublishRowsReset()
	d.showPreview()
}

func (d *dupesPage) relationText(r dupeRow) string {
	action := "남김"
	if d.checked[r.f] {
		action = "정리"
	}
	if r.f == r.g.Keep() {
		return "기준 파일 · " + action
	}
	same, sim := r.g.Relation(r.f)
	switch {
	case same:
		return "똑같은 파일 · " + action
	case sim >= 100:
		return "모습이 같은 사진 · " + action
	}
	return fmt.Sprintf("%d%% 비슷 · %s", sim, action)
}

// ---------- checking ----------

func (d *dupesPage) setChecked(f *dupes.File, on bool) {
	if d.busy {
		return
	}
	if on {
		d.checked[f] = true
	} else {
		delete(d.checked, f)
	}
	d.flash = ""
	d.refreshRows()
	d.updateChrome()
}

func (d *dupesPage) refreshRows() {
	if n := len(d.model.rows); n > 0 {
		d.model.PublishRowsChanged(0, n-1)
	}
}

func (d *dupesPage) currentRow() (dupeRow, bool) {
	i := d.tv.CurrentIndex()
	if i < 0 || i >= len(d.model.rows) {
		return dupeRow{}, false
	}
	return d.model.rows[i], true
}

func (d *dupesPage) checkSelected(on bool) {
	if d.busy {
		return
	}
	for _, i := range d.tv.SelectedIndexes() {
		if i >= 0 && i < len(d.model.rows) {
			if on {
				d.checked[d.model.rows[i].f] = true
			} else {
				delete(d.checked, d.model.rows[i].f)
			}
		}
	}
	d.flash = ""
	d.refreshRows()
	d.updateChrome()
}

func (d *dupesPage) keepOnlyCurrent() {
	r, ok := d.currentRow()
	if !ok || d.busy {
		return
	}
	for _, f := range r.g.Files {
		d.checked[f] = f != r.f
	}
	delete(d.checked, r.f)
	d.refreshRows()
	d.updateChrome()
}

func (d *dupesPage) keepGroupOfCurrent() {
	r, ok := d.currentRow()
	if !ok || d.busy {
		return
	}
	for _, f := range r.g.Files {
		delete(d.checked, f)
	}
	d.refreshRows()
	d.updateChrome()
}

func (d *dupesPage) uncheckAll() {
	if d.busy {
		return
	}
	d.checked = map[*dupes.File]bool{}
	d.refreshRows()
	d.updateChrome()
}

// selection returns the checked files in table order and their total size.
func (d *dupesPage) selection() ([]*dupes.File, int64) {
	var files []*dupes.File
	var bytes int64
	for _, r := range d.model.rows {
		if d.checked[r.f] {
			files = append(files, r.f)
			bytes += r.f.Size
		}
	}
	return files, bytes
}

// fullyChecked counts groups whose every file is checked.
func (d *dupesPage) fullyChecked() int {
	if d.result == nil {
		return 0
	}
	n := 0
	for _, g := range d.result.Groups {
		all := true
		for _, f := range g.Files {
			if !d.checked[f] {
				all = false
				break
			}
		}
		if all {
			n++
		}
	}
	return n
}

// ---------- screen state ----------

func (d *dupesPage) setBusy(b bool) {
	d.busy = b
	for _, w := range []walk.Widget{d.lbRoots, d.btnAddRoot, d.btnRemoveRoot, d.cbExact, d.cbSimilar, d.cbRecursive, d.cmbKind, d.cmbLevel, d.cmbKeep, d.btnUncheckAll} {
		w.SetEnabled(!b)
	}
	d.tv.SetEnabled(!b)
	d.progress.SetVisible(b)
	d.btnCancel.SetVisible(b && d.cancel != nil)
	if b {
		d.progress.SetValue(0)
	}
	d.bottomBar.Invalidate()
}

func (d *dupesPage) updateChrome() {
	hasRows := len(d.model.rows) > 0
	d.tv.SetVisible(hasRows)
	d.emptyHint.SetVisible(!hasRows)
	switch {
	case d.busy && d.cancel != nil:
		d.emptyTitle.SetText("찾는 중이에요…")
		d.emptyText.SetText(d.progText)
	case d.scanned && d.result != nil && len(d.result.Groups) == 0 && d.cleaned > 0:
		d.emptyTitle.SetText("정리를 마쳤어요")
		d.emptyText.SetText("[중복 찾기]를 다시 누르면 남은 중복이 있는지 확인할 수 있어요.")
	case d.scanned && d.result != nil && len(d.result.Groups) == 0:
		d.emptyTitle.SetText("중복 파일이 없어요")
		d.emptyText.SetText("다른 폴더를 추가하거나, '비슷한 정도'를 바꿔서 다시 찾아 보세요.")
	case len(d.roots) == 0:
		d.emptyTitle.SetText("중복 파일을 찾을 폴더를 추가하세요")
		d.emptyText.SetText("[폴더 추가]를 누르거나, 폴더를 이 창으로 끌어다 놓으세요.")
	default:
		d.emptyTitle.SetText("[중복 찾기]를 누르세요")
		d.emptyText.SetText("똑같은 파일은 내용을, 비슷한 사진은 사진의 모습을 비교해서 찾아요.")
	}

	files, bytes := d.selection()
	groups := 0
	var reclaim int64
	if d.result != nil {
		groups = len(d.result.Groups)
		for _, g := range d.result.Groups {
			reclaim += g.Reclaimable()
		}
	}
	if groups > 0 {
		d.resultInfo.SetText(fmt.Sprintf("묶음 %s개 · 모두 정리하면 %s를 비울 수 있어요", groupDigits(groups), formatSize(reclaim)))
	} else {
		d.resultInfo.SetText("")
	}

	text, color := "", colText
	switch {
	case d.busy:
		text = d.progText
	case d.flash != "":
		text = d.flash
		if d.flashOK {
			color = colGreen
		}
	case groups > 0:
		text = fmt.Sprintf("체크한 파일 %s개 (%s)", groupDigits(len(files)), formatSize(bytes))
		if n := d.fullyChecked(); n > 0 {
			text += fmt.Sprintf(" · 주의: 묶음 %d개는 모든 파일이 체크돼 있어요", n)
			color = colRed
		}
	case len(d.roots) == 0:
		text = "폴더를 추가하고 [중복 찾기]를 누르세요."
	default:
		text = "찾을 준비가 됐어요. [중복 찾기]를 누르세요."
	}
	if d.status.Text() != text {
		d.status.SetText(text)
		d.status.Invalidate()
		d.bottomBar.Invalidate()
	}
	d.status.SetTextColor(color)

	idle := !d.busy
	d.btnScan.SetEnabled(idle && len(d.roots) > 0)
	d.btnRemoveRoot.SetEnabled(idle && len(d.roots) > 0)
	d.btnTrash.SetEnabled(idle && len(files) > 0)
	d.btnMove.SetEnabled(idle && len(files) > 0)
	d.btnUndo.SetEnabled(idle && len(d.undoMoves) > 0)
	d.btnUncheckAll.SetEnabled(idle && len(files) > 0)
	if len(files) > 0 {
		d.btnTrash.SetText(fmt.Sprintf("휴지통으로 보내기 (%s)", groupDigits(len(files))))
	} else {
		d.btnTrash.SetText("휴지통으로 보내기")
	}
	d.btnCancel.SetVisible(d.busy && d.cancel != nil)
}

// ---------- preview ----------

func (d *dupesPage) showPreview() {
	d.previewSeq++
	seq := d.previewSeq
	r, ok := d.currentRow()
	if !ok {
		d.setPreviewImage(nil)
		d.lblPName.SetText("")
		d.lblPInfo.SetText("")
		d.lblPDate.SetText("")
		d.lblPFolder.SetText("")
		d.lblPNote.SetText("목록에서 파일을 고르면 여기에 보여요.")
		d.btnOpen.SetEnabled(false)
		d.btnReveal.SetEnabled(false)
		return
	}
	f := r.f
	d.lblPName.SetText(f.Name)
	info := formatSize(f.Size)
	if f.W > 0 {
		info += fmt.Sprintf(" · %d × %d", f.W, f.H)
	}
	d.lblPInfo.SetText(info)
	date := "수정한 날짜 " + formatTime(f.Modified)
	if !f.Taken.IsZero() {
		date = "찍은 날짜 " + formatTime(f.Taken)
	}
	d.lblPDate.SetText(date)
	d.lblPFolder.SetText(f.Dir)
	d.btnOpen.SetEnabled(true)
	d.btnReveal.SetEnabled(true)
	if !dupes.IsPhoto(f.Name) {
		d.setPreviewImage(nil)
		d.lblPNote.SetText("사진이 아니라서 미리 보기는 없어요.")
		return
	}
	d.lblPNote.SetText("불러오는 중…")
	path := f.Path
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		l := dupes.NewLoader()
		img, err := dupes.Preview(l, path, 560)
		l.Close()
		d.a.app.Synchronize(func() {
			if seq != d.previewSeq {
				return
			}
			if err != nil {
				d.setPreviewImage(nil)
				d.lblPNote.SetText("이 사진은 미리 볼 수 없어요.")
				return
			}
			d.setPreviewImage(img)
			d.lblPNote.SetText("")
		})
	}()
}

// The preview is drawn on a canvas of this size (in 96-DPI units).
const previewW, previewH = 276, 207

// setPreviewImage shows img fitted on the preview canvas (nil: empty canvas).
func (d *dupesPage) setPreviewImage(img image.Image) {
	dpi := d.iv.DPI()
	if dpi <= 0 {
		dpi = 96
	}
	cw, ch := previewW*dpi/96, previewH*dpi/96
	canvas := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	bg := color.NRGBA{R: colBg.R(), G: colBg.G(), B: colBg.B(), A: 255}
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	if img != nil && img.Bounds().Dx() > 0 && img.Bounds().Dy() > 0 {
		iw, ih := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
		scale := min(float64(cw)/iw, float64(ch)/ih, 1)
		small := dupes.Shrink(img, max(1, int(max(iw, ih)*scale)))
		b := small.Bounds()
		at := image.Pt((cw-b.Dx())/2, (ch-b.Dy())/2)
		draw.Draw(canvas, image.Rectangle{Min: at, Max: at.Add(b.Size())}, small, b.Min, draw.Over)
	}
	bmp, err := walk.NewBitmapFromImageForDPI(canvas, dpi)
	if err != nil {
		return
	}
	d.iv.SetImage(bmp)
	if d.previewBmp != nil {
		d.previewBmp.Dispose()
	}
	d.previewBmp = bmp
}

func (d *dupesPage) openCurrent() {
	if r, ok := d.currentRow(); ok {
		openURL(r.f.Path)
	}
}

func (d *dupesPage) revealCurrent() {
	if r, ok := d.currentRow(); ok {
		openInExplorer(r.f.Path, true)
	}
}

// ---------- cleaning up ----------

func (d *dupesPage) confirmCleanup(what string) ([]*dupes.File, bool) {
	files, bytes := d.selection()
	if len(files) == 0 || d.busy {
		return nil, false
	}
	msg := fmt.Sprintf("체크한 파일 %s개 (%s)를 %s.", groupDigits(len(files)), formatSize(bytes), what)
	if n := d.fullyChecked(); n > 0 {
		msg += fmt.Sprintf("\n\n주의: 묶음 %d개는 모든 파일이 체크돼 있어서, 그 묶음에는 남는 파일이 없어요.", n)
	}
	msg += "\n\n계속할까요?"
	if walk.MsgBox(d.a.mw, appName, msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != win.IDYES {
		return nil, false
	}
	return files, true
}

func (d *dupesPage) recycle() {
	files, ok := d.confirmCleanup("휴지통으로 보낼게요\n(휴지통에서 언제든 되살릴 수 있어요)")
	if !ok {
		return
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	d.setBusy(true)
	d.progText = "휴지통으로 보내는 중이에요…"
	d.progress.SetRange(0, len(paths))
	d.updateChrome()
	owner := uintptr(d.a.mw.Handle())
	go func() {
		gone, canceled := dupes.Recycle(owner, paths, func(done, total int) {
			d.a.app.Synchronize(func() {
				d.progress.SetValue(done)
				d.progText = fmt.Sprintf("휴지통으로 보내는 중이에요… %s / %s", groupDigits(done), groupDigits(total))
				d.updateChrome()
			})
		})
		d.a.app.Synchronize(func() {
			var done []*dupes.File
			var failed []string
			var bytes int64
			for i, f := range files {
				if gone[i] {
					done = append(done, f)
					bytes += f.Size
				} else if !canceled {
					failed = append(failed, f.Path)
				}
			}
			d.writeLog("휴지통", files, gone, nil)
			d.undoMoves, d.undoGroups = nil, nil
			d.removeFromResults(done)
			d.cleaned += len(done)
			d.setBusy(false)
			d.flash = fmt.Sprintf("%s개 (%s)를 휴지통으로 보냈어요. 휴지통에서 되살릴 수 있어요.", groupDigits(len(done)), formatSize(bytes))
			d.flashOK = len(done) > 0
			if canceled {
				d.flash += " (중간에 멈췄어요)"
			}
			d.rebuildRows()
			d.updateChrome()
			if len(failed) > 0 {
				walk.MsgBox(d.a.mw, appName, fmt.Sprintf("%d개는 휴지통으로 보내지 못했어요 (다른 프로그램이 쓰고 있거나 권한이 없어요):\n\n%s",
					len(failed), strings.Join(limitLines(failed, 10), "\n")), walk.MsgBoxIconWarning)
			}
		})
	}()
}

func (d *dupesPage) moveToFolder() {
	if d.busy {
		return
	}
	start := d.a.settings.LastDir
	paths, err := pickPaths(windows.HWND(d.a.mw.Handle()), true, "체크한 파일을 옮길 폴더 고르기", "이 폴더로 옮기기", start)
	if err != nil {
		walk.MsgBox(d.a.mw, appName, err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if len(paths) == 0 {
		return
	}
	dest := paths[0]
	files, ok := d.confirmCleanup("아래 폴더로 옮길게요\n" + dest + "\n(같은 이름이 있으면 뒤에 (2), (3)… 을 붙여요)")
	if !ok {
		return
	}
	src := make([]string, len(files))
	for i, f := range files {
		src[i] = f.Path
	}
	// remember the groups as they are, for 되돌리기
	snapGroups := d.snapshotGroups()
	snapCheck := map[*dupes.File]bool{}
	for f, v := range d.checked {
		snapCheck[f] = v
	}
	d.setBusy(true)
	d.progText = "옮기는 중이에요…"
	d.progress.SetRange(0, len(src))
	d.updateChrome()
	go func() {
		moves := dupes.MoveTo(src, dest, func(done, total int) {
			d.a.app.Synchronize(func() {
				d.progress.SetValue(done)
				d.progText = fmt.Sprintf("옮기는 중이에요… %s / %s", groupDigits(done), groupDigits(total))
				d.updateChrome()
			})
		})
		d.a.app.Synchronize(func() {
			ok := make([]bool, len(files))
			var moved []*dupes.File
			var failed []string
			for i, m := range moves {
				if m.Err == nil {
					ok[i] = true
					moved = append(moved, files[i])
				} else {
					failed = append(failed, filepath.Base(m.From)+" — "+describeMoveError(m.Err))
				}
			}
			d.writeLog("옮김", files, ok, moves)
			d.undoMoves, d.undoGroups, d.undoCheck = moves, snapGroups, snapCheck
			d.removeFromResults(moved)
			d.cleaned += len(moved)
			d.setBusy(false)
			d.flash = fmt.Sprintf("%s개를 옮겼어요. 잘못됐다면 [되돌리기]를 누르세요.", groupDigits(len(moved)))
			d.flashOK = len(moved) > 0
			d.a.settings.LastDir = dest
			d.rebuildRows()
			d.updateChrome()
			if len(failed) > 0 {
				walk.MsgBox(d.a.mw, appName, fmt.Sprintf("%d개는 옮기지 못했어요:\n\n%s", len(failed), strings.Join(limitLines(failed, 10), "\n")), walk.MsgBoxIconWarning)
			}
		})
	}()
}

func (d *dupesPage) undo() {
	if d.busy || len(d.undoMoves) == 0 {
		return
	}
	moves := d.undoMoves
	d.setBusy(true)
	d.progText = "되돌리는 중이에요…"
	d.updateChrome()
	go func() {
		back := dupes.MoveBack(moves, nil)
		d.a.app.Synchronize(func() {
			var failed []string
			for _, m := range back {
				if m.Err != nil {
					failed = append(failed, filepath.Base(m.To)+" — "+describeMoveError(m.Err))
				}
			}
			if d.result != nil && d.undoGroups != nil {
				d.result.Groups = d.undoGroups
				d.checked = d.undoCheck
				d.cleaned = 0
			}
			d.undoMoves, d.undoGroups, d.undoCheck = nil, nil, nil
			d.setBusy(false)
			d.flash = fmt.Sprintf("%s개를 원래 자리로 되돌렸어요.", groupDigits(len(back)-len(failed)))
			d.flashOK = len(failed) == 0
			d.rebuildRows()
			d.updateChrome()
			if len(failed) > 0 {
				walk.MsgBox(d.a.mw, appName, fmt.Sprintf("%d개는 되돌리지 못했어요:\n\n%s", len(failed), strings.Join(limitLines(failed, 10), "\n")), walk.MsgBoxIconWarning)
			}
		})
	}()
}

func describeMoveError(err error) string {
	var le *os.LinkError
	if errors.As(err, &le) {
		err = le.Err
	}
	switch {
	case errors.Is(err, os.ErrExist), errors.Is(err, windows.ERROR_ALREADY_EXISTS), errors.Is(err, windows.ERROR_FILE_EXISTS):
		return "같은 이름의 파일이 이미 있어요"
	case errors.Is(err, os.ErrNotExist):
		return "파일을 찾을 수 없어요"
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return "다른 프로그램이 쓰고 있거나 권한이 없어요"
	case errors.Is(err, windows.ERROR_DISK_FULL):
		return "저장 공간이 부족해요"
	}
	return err.Error()
}

func (d *dupesPage) snapshotGroups() []*dupes.Group {
	if d.result == nil {
		return nil
	}
	out := make([]*dupes.Group, len(d.result.Groups))
	for i, g := range d.result.Groups {
		out[i] = &dupes.Group{Kind: g.Kind, Files: append([]*dupes.File(nil), g.Files...)}
	}
	return out
}

// removeFromResults drops cleaned-up files; groups left with one file go.
func (d *dupesPage) removeFromResults(done []*dupes.File) {
	if d.result == nil || len(done) == 0 {
		return
	}
	gone := map[*dupes.File]bool{}
	for _, f := range done {
		gone[f] = true
		delete(d.checked, f)
	}
	var groups []*dupes.Group
	for _, g := range d.result.Groups {
		var keep []*dupes.File
		for _, f := range g.Files {
			if !gone[f] {
				keep = append(keep, f)
			}
		}
		if len(keep) > 1 {
			groups = append(groups, &dupes.Group{Kind: g.Kind, Files: keep})
		} else {
			for _, f := range keep {
				delete(d.checked, f)
			}
		}
	}
	d.result.Groups = groups
}

// writeLog keeps a CSV of what was cleaned up, next to the rename logs.
func (d *dupesPage) writeLog(action string, files []*dupes.File, ok []bool, moves []dupes.Moved) {
	dir := logDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "중복 정리 "+time.Now().Format("2006-01-02_150405")+".csv")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(f)
	w.Write([]string{"작업", "파일", "크기", "옮긴 곳", "결과"})
	for i, file := range files {
		res := "완료"
		if !ok[i] {
			res = "실패"
		}
		to := ""
		if i < len(moves) {
			to = moves[i].To
		}
		w.Write([]string{action, file.Path, fmt.Sprint(file.Size), to, res})
	}
	w.Flush()
}

// ---------- table events ----------

func (d *dupesPage) styleCell(s *walk.CellStyle) {
	row, col := s.Row(), s.Col()
	if row < 0 || row >= len(d.model.rows) {
		return
	}
	r := d.model.rows[row]
	if r.gi%2 == 1 {
		s.BackgroundColor = colGroupBg
	}
	switch col {
	case dcRelation:
		if d.checked[r.f] {
			s.TextColor = colRed
		} else {
			s.TextColor = colGreen
		}
	case dcGroup:
		s.TextColor = colMuted
		if d.boldFont != nil {
			s.Font = d.boldFont
		}
	case dcName:
		if r.f == r.g.Keep() && d.boldFont != nil {
			s.Font = d.boldFont
		}
	}
}

func (d *dupesPage) onTableKey(key walk.Key) {
	switch {
	case key == walk.KeyReturn:
		d.openCurrent()
	case key == walk.KeyA && walk.ControlDown():
		all := make([]int, len(d.model.rows))
		for i := range all {
			all[i] = i
		}
		d.tv.SetSelectedIndexes(all)
	case key == walk.KeyZ && walk.ControlDown():
		d.undo()
	}
}

// groupDigits writes 12345 as 12,345.
func groupDigits(n int) string {
	s := itoa(n)
	if n < 0 || len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

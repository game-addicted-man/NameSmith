//go:build windows

package ui

import (
	"github.com/tailscale/walk"
	. "github.com/tailscale/walk/declarative"

	"github.com/game-addicted-man/NameSmith/internal/engine"
)

var (
	colBg     = walk.RGB(242, 244, 247)
	colCard   = walk.RGB(255, 255, 255)
	colLine   = walk.RGB(226, 229, 234)
	colText   = walk.RGB(31, 41, 55)
	colMuted  = walk.RGB(107, 114, 128)
	colAccent = walk.RGB(29, 78, 216)
	colGreen  = walk.RGB(21, 128, 61)
	colRed    = walk.RGB(185, 28, 28)
		colGrayFg = walk.RGB(150, 155, 162)
)

const uiFont = "Malgun Gothic"

var (
	fontBase   = Font{Family: uiFont, PointSize: 9}
	fontSmall  = Font{Family: uiFont, PointSize: 8}
	fontHeader = Font{Family: uiFont, PointSize: 10, Bold: true}
	fontTitle  = Font{Family: uiFont, PointSize: 15, Bold: true}
	fontHint   = Font{Family: uiFont, PointSize: 13, Bold: true}
	fontButton = Font{Family: uiFont, PointSize: 10, Bold: true}
)

// card is one rule section: a bold check box title, a short explanation
// and the fields.
func card(title, desc string, header **walk.CheckBox, onToggle func(), body ...Widget) Widget {
	children := []Widget{
		CheckBox{AssignTo: header, Text: title, Font: fontHeader, OnCheckedChanged: onToggle},
		Label{Text: desc, TextColor: colMuted, Font: fontSmall},
	}
	children = append(children, body...)
	return Composite{
		Background: SolidColorBrush{Color: colCard},
		Layout:     VBox{Margins: Margins{Left: 14, Top: 10, Right: 14, Bottom: 12}, Spacing: 6, Alignment: AlignHNearVNear},
		Children:   children,
	}
}

func grid(children ...Widget) Widget {
	return Composite{
		Layout:   Grid{Columns: 2, MarginsZero: true, Spacing: 6},
		Children: children,
	}
}

func row(children ...Widget) Widget {
	return Composite{
		Layout:   HBox{MarginsZero: true, Spacing: 8},
		Children: children,
	}
}

func fieldLabel(text string) Widget {
	return Label{Text: text, MinSize: Size{Width: 64}}
}

func (a *App) build() error {
	sections := []Widget{
		card("1. 찾아 바꾸기", "이름 속 글자를 찾아 다른 글자로 바꾸거나 지워요.", &a.cbReplace, a.changed,
			grid(
				fieldLabel("찾을 글자"),
				LineEdit{AssignTo: &a.leFind, CueBanner: "예: IMG_ 또는 - 복사본", OnTextChanged: func() { a.touch(a.cbReplace) }},
				fieldLabel("바꿀 글자"),
				LineEdit{AssignTo: &a.leWith, CueBanner: "비워 두면 찾은 글자를 지워요", OnTextChanged: func() { a.touch(a.cbReplace) }},
			),
			row(
				CheckBox{AssignTo: &a.cbMatchCase, Text: "대·소문자 구분", OnCheckedChanged: func() { a.touch(a.cbReplace) }},
				CheckBox{AssignTo: &a.cbIncludeExt, Text: "확장자에도 적용", OnCheckedChanged: func() { a.touch(a.cbReplace) }},
				CheckBox{AssignTo: &a.cbRegex, Text: "정규식",
					ToolTipText: "고급: 정규 표현식으로 찾아요. 바꿀 글자에 $1, $2로 괄호 안 내용을 쓸 수 있어요.",
					OnCheckedChanged: func() { a.touch(a.cbReplace) }},
				HSpacer{},
			),
		),
		card("2. 새 이름 · 글자 추가", "이름을 통째로 바꾸거나, 앞뒤에 글자를 붙여요.", &a.cbText, a.changed,
			row(
				CheckBox{AssignTo: &a.cbSetName, Text: "이름을 통째로", OnCheckedChanged: func() { a.touch(a.cbText) }},
				LineEdit{AssignTo: &a.leNewName, CueBanner: "예: 제주여행", OnTextChanged: func() {
					if !a.loading && a.leNewName.Text() != "" && !a.cbSetName.Checked() {
						a.cbSetName.SetChecked(true)
					}
					a.touch(a.cbText)
				}},
			),
			grid(
				fieldLabel("앞에 붙이기"),
				LineEdit{AssignTo: &a.lePrefix, CueBanner: "예: [회사] ", OnTextChanged: func() { a.touch(a.cbText) }},
				fieldLabel("뒤에 붙이기"),
				LineEdit{AssignTo: &a.leSuffix, CueBanner: "예:  (최종)", OnTextChanged: func() { a.touch(a.cbText) }},
			),
		),
		card("3. 날짜 넣기", "파일의 날짜를 이름 앞이나 뒤에 넣어요.", &a.cbDate, a.changed,
			grid(
				fieldLabel("어떤 날짜"),
				ComboBox{AssignTo: &a.cmbDateSource,
					Model:                 []string{"수정한 날짜", "만든 날짜", "찍은 날짜 (사진·동영상)", "오늘 날짜"},
					ToolTipText:           "찍은 날짜: 사진/동영상 안에 저장된 촬영 날짜예요. 없는 파일은 수정한 날짜를 써요.",
					OnCurrentIndexChanged: func() { a.touch(a.cbDate) }},
				fieldLabel("형식"),
				ComboBox{AssignTo: &a.cmbDateFormat, Editable: true,
					Model:       datePresetFormats(),
					ToolTipText: "YYYY 연도 · MM 월 · DD 일 · HH 시 · mm 분 · ss 초 · ddd 요일\n직접 입력해도 돼요. 예: YYYY년 M월 D일",
					OnTextChanged:         func() { a.touch(a.cbDate) },
					OnCurrentIndexChanged: func() { a.touch(a.cbDate) }},
				Label{Text: ""},
				Label{AssignTo: &a.lblDateExample, TextColor: colMuted, Font: fontSmall},
				fieldLabel("위치"),
				row(
					Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
						RadioButton{AssignTo: &a.rbDateBefore, Text: "이름 앞", OnClicked: func() { a.touch(a.cbDate) }},
						RadioButton{AssignTo: &a.rbDateAfter, Text: "이름 뒤", OnClicked: func() { a.touch(a.cbDate) }},
					}},
					Label{Text: "  구분"},
					LineEdit{AssignTo: &a.leDateSep, MaxSize: Size{Width: 44}, MinSize: Size{Width: 44}, OnTextChanged: func() { a.touch(a.cbDate) }},
					HSpacer{},
				),
			),
		),
		card("4. 번호 붙이기", "목록 순서대로 001, 002, 003…을 붙여요.", &a.cbNumber, a.changed,
			row(
				Label{Text: "시작"},
				NumberEdit{AssignTo: &a.neStart, MinValue: 0, MaxValue: 999999999, Decimals: 0, Increment: 1, SpinButtonsVisible: true,
					MinSize: Size{Width: 70}, MaxSize: Size{Width: 70}, OnValueChanged: func() { a.touch(a.cbNumber) }},
				Label{Text: "증가"},
				NumberEdit{AssignTo: &a.neStep, MinValue: 1, MaxValue: 10000, Decimals: 0, Increment: 1, SpinButtonsVisible: true,
					MinSize: Size{Width: 60}, MaxSize: Size{Width: 60}, OnValueChanged: func() { a.touch(a.cbNumber) }},
				Label{Text: "자릿수"},
				NumberEdit{AssignTo: &a.neDigits, MinValue: 1, MaxValue: 10, Decimals: 0, Increment: 1, SpinButtonsVisible: true,
					MinSize: Size{Width: 52}, MaxSize: Size{Width: 52}, OnValueChanged: func() { a.touch(a.cbNumber) }},
				HSpacer{},
			),
			Label{AssignTo: &a.lblNumExample, TextColor: colMuted, Font: fontSmall},
			row(
				Label{Text: "위치"},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
					RadioButton{AssignTo: &a.rbNumAfter, Text: "이름 뒤", OnClicked: func() { a.touch(a.cbNumber) }},
					RadioButton{AssignTo: &a.rbNumBefore, Text: "이름 앞", OnClicked: func() { a.touch(a.cbNumber) }},
				}},
				Label{Text: "  구분"},
				LineEdit{AssignTo: &a.leNumSep, MaxSize: Size{Width: 44}, MinSize: Size{Width: 44}, OnTextChanged: func() { a.touch(a.cbNumber) }},
				HSpacer{},
			),
			CheckBox{AssignTo: &a.cbPerFolder, Text: "폴더마다 번호를 처음부터 다시", OnCheckedChanged: func() { a.touch(a.cbNumber) }},
			Label{Text: "순서는 왼쪽 목록 그대로예요. 열 제목을 누르거나 ▲▼로 바꾸세요.", TextColor: colMuted, Font: fontSmall},
		),
		card("5. 기타", "대·소문자, 확장자, 맥에서 온 파일의 깨진 한글을 정리해요.", &a.cbExtra, a.changed,
			grid(
				fieldLabel("대·소문자"),
				ComboBox{AssignTo: &a.cmbCase,
					Model:                 []string{"그대로", "모두 소문자 (abc)", "모두 대문자 (ABC)", "단어 첫 글자만 대문자 (Abc)"},
					OnCurrentIndexChanged: func() { a.touch(a.cbExtra) }},
				fieldLabel("확장자"),
				row(
					ComboBox{AssignTo: &a.cmbExt,
						Model:                 []string{"그대로", "소문자로 (.jpg)", "대문자로 (.JPG)", "다른 확장자로 →", "확장자 없애기"},
						OnCurrentIndexChanged: func() { a.touch(a.cbExtra) }},
					LineEdit{AssignTo: &a.leNewExt, CueBanner: "jpg", MinSize: Size{Width: 60}, MaxSize: Size{Width: 70},
						OnTextChanged: func() {
							if !a.loading && a.leNewExt.Text() != "" && a.cmbExt.CurrentIndex() != int(engine.ExtSet) {
								a.cmbExt.SetCurrentIndex(int(engine.ExtSet))
							}
							a.touch(a.cbExtra)
						}},
				),
			),
			CheckBox{AssignTo: &a.cbHangul, Text: "자음·모음이 분리된 한글 합치기 (ㅅㅏㅈㅣㄴ → 사진)", OnCheckedChanged: func() { a.touch(a.cbExtra) }},
		),
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 8},
			Children: []Widget{
				HSpacer{},
				PushButton{AssignTo: &a.btnRulesOff, Text: "모두 끄기", OnClicked: func() { a.rulesOff(); a.changed() }},
				PushButton{AssignTo: &a.btnRulesReset, Text: "처음 설정으로", OnClicked: func() {
					a.applyRules(engine.DefaultRules())
					a.changed()
				}},
			},
		},
		VSpacer{},
	}

	tableMenu := []MenuItem{
		Action{Text: "체크하기", OnTriggered: func() { a.setChecked(true) }},
		Action{Text: "체크 해제 (이름 바꾸기에서 빼기)", OnTriggered: func() { a.setChecked(false) }},
		Separator{},
		Action{Text: "맨 위로", OnTriggered: func() { a.move(-2) }},
		Action{Text: "위로", OnTriggered: func() { a.move(-1) }},
		Action{Text: "아래로", OnTriggered: func() { a.move(1) }},
		Action{Text: "맨 아래로", OnTriggered: func() { a.move(2) }},
		Separator{},
		Menu{Text: "정렬", Items: []MenuItem{
			Action{Text: "이름순 (가→하, 1→10)", OnTriggered: func() { a.sortBy(colName, walk.SortAscending) }},
			Action{Text: "이름 역순", OnTriggered: func() { a.sortBy(colName, walk.SortDescending) }},
			Action{Text: "수정한 날짜순 (오래된 것부터)", OnTriggered: func() { a.sortBy(colModified, walk.SortAscending) }},
			Action{Text: "수정한 날짜순 (최신부터)", OnTriggered: func() { a.sortBy(colModified, walk.SortDescending) }},
			Action{Text: "크기순 (작은 것부터)", OnTriggered: func() { a.sortBy(colSize, walk.SortAscending) }},
			Action{Text: "폴더순", OnTriggered: func() { a.sortBy(colFolder, walk.SortAscending) }},
		}},
		Separator{},
		Action{Text: "모두 선택\tCtrl+A", OnTriggered: a.selectAll},
		Action{Text: "목록에서 빼기\tDelete", OnTriggered: a.removeSelected},
		Action{Text: "파일 위치 열기", OnTriggered: a.openSelectedFolder},
		Action{Text: "파일 정보 새로 읽기\tF5", OnTriggered: a.reloadFromDisk},
	}

	return MainWindow{
		AssignTo:   &a.mw,
		Title:      appTitle,
		Size:       Size{Width: 1240, Height: 800},
		MinSize:    Size{Width: 900, Height: 580},
		Visible:    false,
		Font:       fontBase,
		Background: SolidColorBrush{Color: colBg},
		Layout:     VBox{MarginsZero: true, SpacingZero: true},
		OnDropFiles: func(files []string) {
			a.addPaths(files)
		},
		Children: []Widget{
			// top bar
			Composite{
				Background: SolidColorBrush{Color: colCard},
				Layout:     HBox{Margins: Margins{Left: 18, Top: 10, Right: 18, Bottom: 10}, Spacing: 10},
				Children: []Widget{
					Composite{
						Layout: VBox{MarginsZero: true, SpacingZero: true},
						Children: []Widget{
							Label{Text: "NameSmith", Font: fontTitle, TextColor: colAccent},
							Label{Text: "파일 이름 일괄 변경기 · 번호 붙이기 · 찾아 바꾸기 · 날짜 넣기", TextColor: colMuted, Font: fontSmall},
						},
					},
					HSpacer{},
					CheckBox{AssignTo: &a.cbRecursive, Text: "폴더 안의 폴더까지 (하위 폴더 포함)"},
					PushButton{AssignTo: &a.btnAddFolder, Text: "폴더 추가…", MinSize: Size{Width: 104, Height: 32}, OnClicked: a.addFolder},
					PushButton{AssignTo: &a.btnAddFiles, Text: "파일 추가…", MinSize: Size{Width: 104, Height: 32}, OnClicked: a.addFiles},
				},
			},
			Composite{Background: SolidColorBrush{Color: colLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},

			// body
			Composite{
				Layout: HBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 10}, Spacing: 12},
				Children: []Widget{
					Composite{
						Background: SolidColorBrush{Color: colCard},
						Layout:     VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 12}, Spacing: 8},
						Children: []Widget{
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									Label{Text: "파일 목록", Font: fontHeader},
									Label{AssignTo: &a.listCount, TextColor: colMuted},
									Label{AssignTo: &a.sortInfo, TextColor: colMuted, Font: fontSmall},
									HSpacer{},
									PushButton{AssignTo: &a.btnUp, Text: "▲ 위로", OnClicked: func() { a.move(-1) }},
									PushButton{AssignTo: &a.btnDown, Text: "▼ 아래로", OnClicked: func() { a.move(1) }},
									PushButton{AssignTo: &a.btnRemove, Text: "목록에서 빼기", OnClicked: a.removeSelected},
									PushButton{AssignTo: &a.btnClear, Text: "모두 비우기", OnClicked: a.clearAll},
								},
							},
							Composite{
								AssignTo: &a.emptyHint,
								Layout:   VBox{Spacing: 10},
								Children: []Widget{
									VSpacer{},
									Label{Text: "파일이나 폴더를 여기로 끌어다 놓으세요", Font: fontHint, TextColor: colMuted, TextAlignment: AlignCenter},
									Label{Text: "또는 위의 [파일 추가] · [폴더 추가] 버튼을 누르세요.", TextColor: colMuted, TextAlignment: AlignCenter},
									Label{Text: "탐색기에서 폴더를 오른쪽 클릭해 'NameSmith로 이름 바꾸기'를 골라도 돼요.", TextColor: colGrayFg, Font: fontSmall, TextAlignment: AlignCenter},
									VSpacer{},
								},
							},
							TableView{
								AssignTo:         &a.tv,
								Visible:          false,
								Model:            a.model,
								CheckBoxes:       true,
								MultiSelection:   true,
								CustomRowHeight:  24,
								ColumnsOrderable: false,
								Columns: []TableViewColumn{
									{Title: "현재 이름", Width: 220},
									{Title: "새 이름", Width: 240},
									{Title: "상태", Width: 170},
									{Title: "수정한 날짜", Width: 116},
									{Title: "크기", Width: 72, Alignment: AlignFar},
									{Title: "폴더", Width: 260},
								},
								StyleCell:                a.styleCell,
								ContextMenuItems:         tableMenu,
								OnKeyDown:                a.onTableKey,
								OnSelectedIndexesChanged: func() { a.updateChrome(a.readRules()) },
								OnItemActivated:          a.openSelectedFolder,
							},
						},
					},
					ScrollView{
						AssignTo:        &a.rulesPanel,
						HorizontalFixed: true,
						MinSize:         Size{Width: 400},
						MaxSize:         Size{Width: 400},
						Layout:          VBox{MarginsZero: true, Spacing: 10},
						Children:        sections,
					},
				},
			},

			// bottom bar
			Composite{Background: SolidColorBrush{Color: colLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},
			Composite{
				AssignTo:   &a.bottomBar,
				Background: SolidColorBrush{Color: colCard},
				Layout:     HBox{Margins: Margins{Left: 18, Top: 10, Right: 18, Bottom: 10}, Spacing: 12},
				Children: []Widget{
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 2, Alignment: AlignHNearVCenter},
						Children: []Widget{
							Label{AssignTo: &a.status, Font: Font{Family: uiFont, PointSize: 10}, MinSize: Size{Width: 560}},
							LinkLabel{
								MaxSize:         Size{Width: 640},
								Font:            fontSmall,
								Text:            `<a id="help">사용법</a>   ·   <a id="logs">바꾼 기록 보기</a>   ·   <a id="site">홈페이지 / 새 버전</a>   ·   v` + Version,
								OnLinkActivated: a.onLink,
							},
						},
					},
					HSpacer{},
					ProgressBar{AssignTo: &a.progress, Visible: false, MinSize: Size{Width: 220, Height: 18}, MaxSize: Size{Width: 220, Height: 18}},
					PushButton{AssignTo: &a.btnUndo, Text: "되돌리기", MinSize: Size{Width: 100, Height: 38}, OnClicked: a.undoLast},
					PushButton{AssignTo: &a.btnRename, Text: "이름 바꾸기", Font: fontButton, MinSize: Size{Width: 190, Height: 38}, OnClicked: a.rename},
				},
			},
		},
	}.Create()
}

func datePresetFormats() []string {
	out := make([]string, len(engine.DatePresets))
	for i, p := range engine.DatePresets {
		out[i] = p.Format
	}
	return out
}

// styleCell colours the new name and status by what will happen.
// (Only text colours: background colours leave marks next to the check
// boxes on some systems.)
func (a *App) styleCell(s *walk.CellStyle) {
	row, col := s.Row(), s.Col()
	if row < 0 || row >= len(a.model.prev) {
		return
	}
	p := a.model.prev[row]
	switch p.Status {
	case engine.StatusConflict, engine.StatusInvalid:
		if col == colNewName || col == colStatus {
			s.TextColor = colRed
		}
	case engine.StatusChanged:
		switch col {
		case colNewName:
			s.TextColor = colAccent
		case colStatus:
			s.TextColor = colGreen
		}
	case engine.StatusSkipped:
		s.TextColor = colGrayFg
	case engine.StatusUnchanged:
		if col == colNewName || col == colStatus {
			s.TextColor = colGrayFg
		}
	}
}

func (a *App) onTableKey(key walk.Key) {
	switch {
	case key == walk.KeyDelete:
		a.removeSelected()
	case key == walk.KeyA && walk.ControlDown():
		a.selectAll()
	case key == walk.KeyZ && walk.ControlDown():
		a.undoLast()
	case key == walk.KeyF5:
		a.reloadFromDisk()
	}
}

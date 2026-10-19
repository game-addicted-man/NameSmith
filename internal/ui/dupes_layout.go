//go:build windows

package ui

import (
	"github.com/tailscale/walk"
	. "github.com/tailscale/walk/declarative"

	"github.com/game-addicted-man/NameSmith/internal/dupes"
)

// widget builds the "중복 파일 찾기" page.
func (d *dupesPage) widget() Widget {
	label := func(text string) Widget {
		return Label{Text: text, Font: fontHeader, MinSize: Size{Width: 74}, MaxSize: Size{Width: 74}}
	}
	tableMenu := []MenuItem{
		Action{Text: "열기\tEnter", OnTriggered: d.openCurrent},
		Action{Text: "파일 위치 열기", OnTriggered: d.revealCurrent},
		Separator{},
		Action{Text: "체크하기 (정리할 파일로)", OnTriggered: func() { d.checkSelected(true) }},
		Action{Text: "체크 해제 (남길 파일로)", OnTriggered: func() { d.checkSelected(false) }},
		Separator{},
		Action{Text: "이 파일만 남기고 묶음의 나머지는 체크", OnTriggered: d.keepOnlyCurrent},
		Action{Text: "이 묶음은 모두 남기기", OnTriggered: d.keepGroupOfCurrent},
	}

	return Composite{
		AssignTo: &d.page,
		Visible:  false,
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{
				Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 10}, Spacing: 12},
				Children: []Widget{
					// what to look for
					Composite{
						Background:      SolidColorBrush{Color: colCard},
						OnBoundsChanged: d.a.repaintSoon,
						MaxSize:         Size{Height: 154},
						Layout:          VBox{Margins: Margins{Left: 14, Top: 12, Right: 14, Bottom: 12}, Spacing: 10},
						Children: []Widget{
							Composite{
								MinSize: Size{Height: 70},
								MaxSize: Size{Height: 70},
								Layout:  HBox{MarginsZero: true, Spacing: 10},
								Children: []Widget{
									Composite{
										Layout: VBox{MarginsZero: true, Spacing: 2, Alignment: AlignHNearVNear},
										Children: []Widget{
											label("찾을 곳"),
										},
									},
									ListBox{
										AssignTo: &d.lbRoots,
										MinSize:  Size{Height: 70},
										MaxSize:  Size{Height: 70},
										OnCurrentIndexChanged: func() {
											d.btnRemoveRoot.SetEnabled(!d.busy && len(d.roots) > 0)
										},
									},
									Composite{
										Layout: VBox{MarginsZero: true, Spacing: 6},
										Children: []Widget{
											PushButton{AssignTo: &d.btnAddRoot, Text: "폴더 추가…", MinSize: Size{Width: 116, Height: 32}, MaxSize: Size{Height: 32}, OnClicked: d.addRootsDialog},
											PushButton{AssignTo: &d.btnRemoveRoot, Text: "목록에서 빼기", MinSize: Size{Width: 116, Height: 32}, MaxSize: Size{Height: 32}, OnClicked: d.removeRoot},
										},
									},
								},
							},
							Composite{
								MinSize: Size{Height: 38},
								MaxSize: Size{Height: 38},
								Layout:  HBox{MarginsZero: true, Spacing: 10},
								Children: []Widget{
									label("찾을 것"),
									CheckBox{AssignTo: &d.cbExact, Text: "내용이 똑같은 파일"},
									ComboBox{AssignTo: &d.cmbKind, Model: dupes.KindLabels, MinSize: Size{Width: 100}, MaxSize: Size{Width: 110},
										ToolTipText: "똑같은 파일을 찾을 파일 종류"},
									Composite{MinSize: Size{Width: 8}, MaxSize: Size{Width: 8}},
									CheckBox{AssignTo: &d.cbSimilar, Text: "비슷한 사진"},
									ComboBox{AssignTo: &d.cmbLevel, Model: dupes.LevelLabels, MinSize: Size{Width: 220},
										ToolTipText: "거의 똑같은 사진만: 크기만 다르거나 다시 저장한 사진\n비슷한 사진: 밝기를 바꾸거나 글자를 넣은 사진까지\n조금 비슷한 사진까지: 살짝 자른 사진, 연이어 찍은 사진까지"},
									Composite{MinSize: Size{Width: 8}, MaxSize: Size{Width: 8}},
									CheckBox{AssignTo: &d.cbRecursive, Text: "하위 폴더 포함"},
									HSpacer{},
									PushButton{AssignTo: &d.btnScan, Text: "중복 찾기", Font: fontButton, MinSize: Size{Width: 150, Height: 34}, MaxSize: Size{Height: 34}, OnClicked: d.startScan},
								},
							},
						},
					},

					// results + preview
					Composite{
						OnBoundsChanged: d.a.repaintSoon,
						Layout:          HBox{MarginsZero: true, Spacing: 12},
						Children: []Widget{
							Composite{
								Background: SolidColorBrush{Color: colCard},
								Layout:     VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 12}, Spacing: 8},
								Children: []Widget{
									Composite{
										Layout: HBox{MarginsZero: true, Spacing: 8},
										Children: []Widget{
											Label{Text: "찾은 결과", Font: fontHeader},
											Label{AssignTo: &d.resultInfo, TextColor: colMuted, EllipsisMode: EllipsisEnd},
											HSpacer{},
											Label{Text: "남길 파일"},
											ComboBox{AssignTo: &d.cmbKeep, Model: dupes.KeepLabels, MinSize: Size{Width: 210},
												OnCurrentIndexChanged: d.onKeepRuleChanged,
												ToolTipText:           "묶음마다 이 기준으로 하나를 남기고, 나머지를 체크해요"},
											PushButton{AssignTo: &d.btnUncheckAll, Text: "체크 모두 해제", OnClicked: d.uncheckAll},
										},
									},
									Label{Text: "체크한 파일이 정리돼요. 묶음마다 '기준 파일' 하나는 남도록 미리 골라 두었어요.", TextColor: colMuted, Font: fontSmall, EllipsisMode: EllipsisEnd},
									Composite{
										AssignTo: &d.emptyHint,
										Layout:   VBox{Spacing: 10},
										Children: []Widget{
											VSpacer{},
											Label{AssignTo: &d.emptyTitle, Font: fontHint, TextColor: colMuted, TextAlignment: AlignCenter},
											Label{AssignTo: &d.emptyText, TextColor: colMuted, TextAlignment: AlignCenter},
											VSpacer{},
										},
									},
									TableView{
										AssignTo:                 &d.tv,
										Visible:                  false,
										Model:                    d.model,
										CheckBoxes:               true,
										MultiSelection:           true,
										CustomRowHeight:          24,
										ColumnsOrderable:         false,
										NotSortableByHeaderClick: true,
										Columns: []TableViewColumn{
											{Title: "이름", Width: 230},
											{Title: "묶음", Width: 48, Alignment: AlignCenter},
											{Title: "결과", Width: 140},
											{Title: "크기", Width: 76, Alignment: AlignFar},
											{Title: "해상도", Width: 96},
											{Title: "수정한 날짜", Width: 116},
											{Title: "폴더", Width: 280},
										},
										StyleCell:             d.styleCell,
										ContextMenuItems:      tableMenu,
										OnKeyDown:             d.onTableKey,
										OnCurrentIndexChanged: d.showPreview,
										OnItemActivated:       d.openCurrent,
									},
								},
							},
							Composite{
								Background: SolidColorBrush{Color: colCard},
								MinSize:    Size{Width: 300},
								MaxSize:    Size{Width: 300},
								Layout:     VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 12}, Spacing: 6},
								Children: []Widget{
									Label{Text: "미리 보기", Font: fontHeader},
									// Ideal mode: the view is exactly as big as the canvas the
									// preview is drawn on, so it never stretches the layout.
									ImageView{
										AssignTo: &d.iv,
										Mode:     ImageViewModeIdeal,
									},
									Label{AssignTo: &d.lblPNote, TextColor: colMuted, Font: fontSmall, EllipsisMode: EllipsisEnd},
									Label{AssignTo: &d.lblPName, Font: fontHeader, EllipsisMode: EllipsisEnd},
									Label{AssignTo: &d.lblPInfo, EllipsisMode: EllipsisEnd},
									Label{AssignTo: &d.lblPDate, TextColor: colMuted, EllipsisMode: EllipsisEnd},
									Label{AssignTo: &d.lblPFolder, TextColor: colMuted, Font: fontSmall, EllipsisMode: EllipsisPath},
									Composite{
										Layout: HBox{MarginsZero: true, Spacing: 6},
										Children: []Widget{
											PushButton{AssignTo: &d.btnOpen, Text: "열기", MinSize: Size{Width: 96}, OnClicked: d.openCurrent},
											PushButton{AssignTo: &d.btnReveal, Text: "파일 위치 열기", MinSize: Size{Width: 120}, OnClicked: d.revealCurrent},
										},
									},
									VSpacer{},
								},
							},
						},
					},
				},
			},

			Composite{Background: SolidColorBrush{Color: colLine}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}},
			Composite{
				AssignTo:   &d.bottomBar,
				Background: SolidColorBrush{Color: colCard},
				Layout:     HBox{Margins: Margins{Left: 18, Top: 10, Right: 18, Bottom: 10}, Spacing: 12},
				Children: []Widget{
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 2, Alignment: AlignHNearVCenter},
						Children: []Widget{
							// the HSpacer lets the status line use all the free width
							Composite{
								Layout: HBox{MarginsZero: true, SpacingZero: true},
								Children: []Widget{
									Label{AssignTo: &d.status, Font: Font{Family: uiFont, PointSize: 10}, MinSize: Size{Width: 300}, EllipsisMode: EllipsisEnd},
									HSpacer{},
								},
							},
							LinkLabel{
								Background:      stockWhite{},
								MaxSize:         Size{Width: 640},
								Font:            fontSmall,
								Text:            `<a id="help">사용법</a>   ·   <a id="logs">정리한 기록 보기</a>   ·   <a id="site">홈페이지 / 새 버전</a>   ·   v` + Version,
								OnLinkActivated: d.a.onLink,
							},
						},
					},
					ProgressBar{AssignTo: &d.progress, Visible: false, MinSize: Size{Width: 200, Height: 18}, MaxSize: Size{Width: 200, Height: 18}},
					PushButton{AssignTo: &d.btnCancel, Text: "멈추기", Visible: false, MinSize: Size{Width: 90, Height: 38}, OnClicked: d.cancelScan},
					PushButton{AssignTo: &d.btnUndo, Text: "되돌리기", MinSize: Size{Width: 90, Height: 38}, OnClicked: d.undo,
						ToolTipText: "다른 폴더로 옮긴 파일을 원래 자리로 돌려놓아요 (휴지통으로 보낸 파일은 휴지통에서 되살리세요)"},
					PushButton{AssignTo: &d.btnMove, Text: "다른 폴더로 옮기기…", MinSize: Size{Width: 150, Height: 38}, OnClicked: d.moveToFolder},
					PushButton{AssignTo: &d.btnTrash, Text: "휴지통으로 보내기", Font: fontButton, MinSize: Size{Width: 200, Height: 38}, OnClicked: d.recycle},
				},
			},
		},
	}
}

var _ = walk.SortAscending

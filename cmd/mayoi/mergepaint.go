package main

import (
	"cmp"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// mergeViewUI shows the three-way merge of a conflicted file: the two
// sides read only beside the result, which the user edits, with the
// conflicts marked in all three, the actions of the one chosen above
// them, and the file's resolution at the end.
func (a *app) mergeViewUI(c *ui.Context, v *mergeView) {
	p := a.pal
	a.refreshMerge(v)
	message := func(msg string) {
		ui.Column(c).Fill().Background(p.editor).Center().Children(func() {
			ui.Text(c, msg).FontSize(13).TextColor(p.muted)
		})
	}
	if v.err != "" {
		message(v.err)
		return
	}
	if !v.loaded || v.doc == nil || v.loading {
		ui.Box(c).Fill().Background(p.editor)
		return
	}
	if v.binary {
		message("The file is binary: the conflict is resolved in the terminal.")
		return
	}
	// The regions follow the result's text, whatever changed it: undo
	// brings the sides it took back, redo puts them in again.
	if ver := v.doc.buf.Version(); ver != v.gen {
		v.syncRegions()
	}
	if v.ed != nil {
		if !a.settings.Vim {
			vimReset(v.ed)
		}
		v.ed.observe()
		if len(v.ed.cmds) > 0 {
			cmds := v.ed.cmds
			v.ed.cmds = nil
			for _, cmd := range cmds {
				cmd(v.ed)
			}
			a.invalidate()
		}
		if v.ed.focusNext {
			v.ed.focusNext = false
			v.panes[1].scroll.Focus(c)
		}
	}
	ui.Column(c).Fill().Background(p.editor).Children(func() {
		a.mergeHead(c, v)
		a.mergeActions(c, v)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.mergePane(c, v, 0, v.oursLabel(), v.oursTip())
			ui.Box(c).Width(1).Shrink(0).Background(p.border)
			a.mergePane(c, v, 1, v.resultLabel(), v.resultTip())
			ui.Box(c).Width(1).Shrink(0).Background(p.border)
			a.mergePane(c, v, 2, v.theirsLabel(), v.theirsTip())
		})
	})
}

// mergeHead is the bar over the panes: the file in conflict, how many of
// its conflicts stand, how many of the files of the merge stand resolved,
// and what ends them.
func (a *app) mergeHead(c *ui.Context, v *mergeView) {
	p := a.pal
	resolved := a.mergeResolved(v)
	filesResolved, filesTotal := a.mergeResolvedCount()
	ui.Row(c).Height(30).Padding(0, 10, 0, 8).Gap(8).AlignItems(ui.Center).Background(p.chrome).Children(func() {
		ui.Icon(c, iconMerge).FontSize(14).TextColor(p.changed)
		ui.Text(c, v.name()).FontSize(12.5).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
		tip := "The file is in conflict"
		if op := v.op.Title(); op != "" {
			tip = "The " + op + " stopped for this file"
		}
		ui.Text(c, tip).FontSize(11).TextColor(p.muted).SingleLine().Shrink(1)
		if n := len(v.regions); n > 0 {
			un := v.unresolved()
			pill := ui.Box(c.Key("merge-count")).Height(16).Padding(0, 6).Radius(8).Shrink(0).
				AlignItems(ui.Center).Justify(ui.Center).
				Label("Conflicts Unresolved").Tooltip("Conflicts the result still has the base's lines for")
			if un > 0 {
				pill.Background(p.danger.Alpha(0.18))
			} else {
				pill.Background(p.text.Alpha(0.07))
			}
			pill.Children(func() {
				ui.RichText(c, ui.Span{Text: fmt.Sprintf("%d of %d", un, n), Size: 10.5, Weight: 600, Color: p.text}).SingleLine()
			})
		}
		if filesTotal > 1 {
			ui.Text(c, fmt.Sprintf("%d of %d files resolved", filesResolved, filesTotal)).
				FontSize(11).TextColor(p.muted).SingleLine().Shrink(0).
				Tooltip("The files of the " + cmp.Or(v.op.Title(), "merge") + " that stand resolved")
		}
		ui.Spacer(c)
		if !resolved && !v.binary && v.loaded {
			b := ui.ButtonBase(c.Key("merge-apply")).Height(20).Padding(0, 8).Radius(5).AlignItems(ui.Center).
				Label("Apply").Tooltip("Take the changes of a side that no conflict of the merge holds into the result")
			if b.Hovered() {
				b.Background(p.hover)
			}
			b.Menu(func(m *ui.Menu) {
				if m.Item("Apply All Non-Conflicting Changes").Chosen() {
					a.post(func() { a.applyNonConflicts(v, applyAll) })
				}
				if m.Item("Apply Non-Conflicting Changes from the Left").Chosen() {
					a.post(func() { a.applyNonConflicts(v, applyLeft) })
				}
				if m.Item("Apply Non-Conflicting Changes from the Right").Chosen() {
					a.post(func() { a.applyNonConflicts(v, applyRight) })
				}
				if m.Item("Apply Automatically").Checked(a.settings.MergeAutoApply).Chosen() {
					a.post(func() { a.setMergeAutoApply(!a.settings.MergeAutoApply) })
				}
			})
			b.Children(func() {
				ui.Text(c, "Apply").FontSize(11.5).FontWeight(500).TextColor(p.text).SingleLine()
			})
		}
		if !resolved {
			if a.mergeButton(c, "merge-reset", "Reset to Base", "Put the base's text in the result, taking back what every conflict took") {
				a.post(func() { a.resetMerge(v) })
			}
		}
		if a.mergeButton(c, "merge-save", "Save", "Save the result in the file (Cmd+S)") {
			a.post(func() { a.saveMerge(v) })
		}
		if !resolved && filesTotal > 1 && a.mergeButton(c, "merge-apply-next", "Apply Changes", "Save the result, stage the file, and go on to the next file in conflict") {
			a.post(func() { a.applyMergeChanges(v) })
		}
		if filesTotal > 0 && a.mergeButton(c, "merge-save-close", "Save and Close", "Save the result, and go back to the files in conflict") {
			a.post(func() { a.saveCloseMerge(v) })
		}
		if filesTotal > 0 && a.mergeButton(c, "merge-files", "Conflicts…", "The files of the "+cmp.Or(v.op.Title(), "merge")+", resolved and to resolve") {
			a.post(a.openConflicts)
		}
		b := ui.PrimaryButton(c.Key("merge-resolved"), "Mark Resolved").Height(22)
		b.Tooltip("Save the result and stage the file, which ends its conflict")
		if b.Clicked() {
			a.post(func() { a.completeMerge(v) })
		}
	})
}

// mergeActions is the bar of the conflict chosen, or of the file once it
// stands resolved.
func (a *app) mergeActions(c *ui.Context, v *mergeView) {
	p := a.pal
	ui.Row(c).Height(26).Padding(0, 10, 2, 8).Gap(6).AlignItems(ui.Center).Background(p.chrome).Children(func() {
		if a.mergeResolved(v) {
			ui.Icon(c, iconCheck).FontSize(13).TextColor(p.added).Shrink(0)
			done := "The file stands resolved."
			if op := v.op.Title(); op != "" {
				done = "The file stands resolved: the " + op + " goes on when every file of it does."
			}
			ui.Text(c, done).FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
			ui.Spacer(c)
			if v.op != "" && a.mergeButton(c, "merge-continue", "Continue "+v.op.Title(), "Go on with the "+v.op.Title()+", once its conflicts stand resolved") {
				a.post(a.continueOp)
			}
			if a.mergeButton(c, "merge-restore", "Restore Conflict…", "Put the file back in conflict, as the command stopped for it has it") {
				a.post(func() { a.restoreMerge(v) })
			}
			if a.mergeButton(c, "merge-open", "Open File", "Open the file as it is on disk") {
				a.post(func() { a.openPath(v.path, true) })
			}
			return
		}
		if a.iconButton(c.Key("merge-prev"), iconArrowUp, "Previous Conflict (])", false).Clicked() {
			a.post(func() { a.stepMergeRegion(v, -1) })
		}
		if a.iconButton(c.Key("merge-next"), iconArrowDown, "Next Conflict ([)", false).Clicked() {
			a.post(func() { a.stepMergeRegion(v, 1) })
		}
		i := v.chosen
		if i < 0 || i >= len(v.regions) {
			ui.Text(c, fmt.Sprintf("%s to resolve", plural(len(v.regions), "conflict"))).FontSize(11.5).TextColor(p.muted).SingleLine()
			ui.Spacer(c)
			return
		}
		r := v.regions[i]
		state := map[regionState]string{
			regionUndecided: "the base's lines",
			regionLeft:      "ours",
			regionRight:     "theirs",
			regionBoth:      "both sides, alike",
			regionEdited:    "lines of its own",
		}[v.stateOf(r)]
		ui.Text(c, fmt.Sprintf("Conflict %d of %d · the result has %s", i+1, len(v.regions), state)).
			FontSize(11.5).TextColor(p.muted).SingleLine().Shrink(1)
		if a.mergeButton(c, "merge-accept-l", "Accept Left", "Put ours' lines of the conflict in the result") {
			a.post(func() { a.takeMergeSide(v, i, true) })
		}
		if a.mergeButton(c, "merge-ignore-l", "Ignore Left", "Put the base's lines of the conflict back, taking ours' out") {
			a.post(func() { a.dropMergeSide(v, i, true) })
		}
		if a.mergeButton(c, "merge-accept-r", "Accept Right", "Put theirs' lines of the conflict in the result") {
			a.post(func() { a.takeMergeSide(v, i, false) })
		}
		if a.mergeButton(c, "merge-ignore-r", "Ignore Right", "Put the base's lines of the conflict back, taking theirs' out") {
			a.post(func() { a.dropMergeSide(v, i, false) })
		}
		ui.Spacer(c)
		if a.mergeButton(c, "merge-use-l", "Resolve using Left", "Accept ours and ignore theirs, for every conflict of the file") {
			a.post(func() { a.resolveUsingMerge(v, true) })
		}
		if a.mergeButton(c, "merge-use-r", "Resolve using Right", "Accept theirs and ignore ours, for every conflict of the file") {
			a.post(func() { a.resolveUsingMerge(v, false) })
		}
	})
}

// mergeButton is a small button of the merge's bars, and reports whether
// it was clicked.
func (a *app) mergeButton(c *ui.Context, key, label, tip string) bool {
	p := a.pal
	b := ui.ButtonBase(c.Key(key)).Height(20).Padding(0, 8).Radius(5).AlignItems(ui.Center).
		Label(label).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.hover)
	}
	col := p.text
	b.Children(func() { ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(col).SingleLine() })
	return b.Clicked()
}

// mergePane is one of the three panes of a merge: its label over a pane
// that scrolls its text both ways, the conflicts marked in it. Pane 1,
// the result, is the one edited.
func (a *app) mergePane(c *ui.Context, v *mergeView, k int, label, tip string) {
	p, f := a.pal, a.font
	buf := v.paneBuf(k)
	if buf == nil {
		return
	}
	ui.Column(c.Key(fmt.Sprintf("merge-pane-%d", k))).Grow(1).MinWidth(0).Children(func() {
		head := ui.Row(c.Key(fmt.Sprintf("merge-head-%d", k))).Height(20).Padding(0, 8, 0, diffRailW+8).
			AlignItems(ui.Center).Background(p.chrome).Label(label).Tooltip(tip)
		head.Children(func() {
			ui.Text(c, label).FontSize(10.5).FontWeight(600).LetterSpacing(0.4).TextColor(p.muted).SingleLine().Shrink(1)
		})
		pane := ui.ScrollHorizontal(c.Key(fmt.Sprintf("merge-scroll-%d", k))).TrackScroll(&v.panes[k].hscroll).
			Grow(1).MinHeight(0).Background(p.editor)
		pane.Children(func() {
			view := v.panes[k].scroll.Bounds(c)
			v.viewW, v.viewH = view.W, view.H
			v.panes[k].viewH = view.H
			ver := buf.Version()
			if v.panes[k].wideVer != ver {
				v.panes[k].wideVer = ver
				v.panes[k].widest = widestOf(v.paneLines(k))
			}
			contentW := max(view.W, textXOf(f, v.panes[k].widest))
			area := ui.TextAreaBufferBase(c, buf).
				Font(f.family).FontSize(f.size).FontWeight(f.weight).FixedLineHeight(f.lineH).TextColor(p.text).
				FillHeight().Padding(editorPadY, textPadR, editorPadY, mergeTextX).
				Label(label).Tooltip(tip)
			if k == 1 {
				vimKeys := a.settings.Vim && v.ed.vim.mode != vimInsert
				area.ReadOnly(vimKeys || a.chording() || v.ed.locked())
				a.takeKeys(area, placeEditor, func(ev ui.InputEvent) bool { return a.editorKey(v.ed, ev) })
				a.mergeResultSync(c, area, ver)
			} else {
				area.ReadOnly(true)
				a.takeKeys(area, placeDiff, func(ev ui.InputEvent) bool { return a.mergePaneKey(v, k, ev) })
			}
			area.NoWrap().Width(contentW).TrackScroll(&v.panes[k].scroll).
				ScrollbarInsets(2, max(0, v.panes[k].hscroll.MaxX-v.panes[k].hscroll.X)+2, 2, 0)
			area.Draw(func(pt *ui.Painter, r ui.Rect) { a.paintMergeRegions(pt, v, k, r) })
		})
	})
}

// mergeResultSync gives the result's text area the editor's selection,
// and the editor the one the area has, as the editor view does.
func (a *app) mergeResultSync(c *ui.Context, area ui.Element, ver uint64) {
	v := a.activeMerge()
	if v == nil || v.ed == nil {
		return
	}
	edited := ver != v.ed.version
	v.ed.version = ver
	if r := v.ed.pending; r != nil {
		area.SetTextSelection(r[0], r[1])
		v.ed.pending, v.ed.resync, v.ed.parked = nil, false, -1
	}
	if v.ed.resync || !v.ed.caretSeen {
		v.ed.resync = false
		area.SetTextSelection(v.ed.sel[0], v.ed.sel[1])
	}
	anchor, caret := area.TextAnchorCaret()
	if sel := [2]int{anchor, caret}; sel != v.ed.sel {
		if !edited {
			v.ed.hist.typing = false // the caret moved: typing after starts a step
		}
		v.ed.sel = sel
		v.ed.caretSeen = true
	}
}

// activeMerge returns the merge view of the tab shown, nil when none is.
func (a *app) activeMerge() *mergeView {
	if t := a.activeTab(); t != nil {
		return t.merge
	}
	return nil
}

// mergePaneKey takes the keys of a read-only pane of a merge: scrolling,
// and going from conflict to conflict.
func (a *app) mergePaneKey(v *mergeView, pane int, ev ui.InputEvent) bool {
	if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
		a.chooseMergeLine(v, pane, ev.Y)
		return false
	}
	if ev.Kind != ui.InputKeyDown || ev.Mods&^ui.Shift != 0 {
		return false
	}
	lineH := a.font.lineH
	page := max(v.panes[pane].viewH-lineH, lineH)
	scroll := func(dy float32) {
		s := &v.panes[pane].scroll
		n := v.paneLineCount(pane)
		end := max(0, 2*editorPadY+float32(n)*lineH-v.panes[pane].viewH)
		s.Y = clamp(s.Y+dy, 0, end)
	}
	switch ev.Key {
	case ui.KeyJ, ui.KeyDown:
		scroll(lineH)
	case ui.KeyK, ui.KeyUp:
		scroll(-lineH)
	case ui.KeyF, ui.KeyPageDown:
		scroll(page)
	case ui.KeyB, ui.KeyPageUp:
		scroll(-page)
	case ui.KeyG:
		if ev.Mods&ui.Shift != 0 {
			scroll(1e9)
		} else {
			scroll(-1e9)
		}
	case ui.KeyBracketRight:
		a.stepMergeRegion(v, 1)
	case ui.KeyBracketLeft:
		a.stepMergeRegion(v, -1)
	default:
		return false
	}
	return true
}

// paintMergeRegions paints the conflicts of a pane: a tint over the
// lines each has there, the deeper for the one chosen, and a rail of the
// state by them.
func (a *app) paintMergeRegions(pt *ui.Painter, v *mergeView, k int, r ui.Rect) {
	p := a.pal
	lineH := a.font.lineH
	sc := v.panes[k].scroll.Y
	first := max(int((sc-editorPadY)/lineH)-1, 0)
	last := first + int(v.panes[k].viewH/lineH) + 3
	for i, reg := range v.regions {
		lo, hi := v.regionLines(reg, k)
		if lo < 0 || hi <= first || lo >= last {
			continue
		}
		if hi == lo {
			hi = lo + 1 // an insertion shows a row of its own
		}
		col := p.added
		switch v.stateOf(reg) {
		case regionUndecided:
			col = p.danger
		case regionEdited:
			col = p.changed
		}
		y := r.Y + editorPadY + float32(lo)*lineH - sc
		h := float32(hi-lo) * lineH
		bg := col.Alpha(0.09)
		if i == v.chosen {
			bg = col.Alpha(0.20)
		}
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: h}, bg, 0)
		rail := col.Alpha(0.5)
		if i == v.chosen {
			rail = col
		}
		pt.Fill(ui.Rect{X: r.X, Y: y, W: diffRailW, H: h}, rail, 0)
	}
}

// paneBuf returns the text buffer a pane shows.
func (v *mergeView) paneBuf(k int) *ui.TextBuffer {
	switch k {
	case 0:
		return v.oursBuf
	case 2:
		return v.theirsBuf
	}
	if v.doc != nil {
		return v.doc.buf
	}
	return nil
}

// paneLines returns the lines a pane shows.
func (v *mergeView) paneLines(k int) []string {
	switch k {
	case 0:
		return v.oursLines
	case 2:
		return v.theirsLines
	}
	if v.doc != nil {
		return v.resultLines()
	}
	return nil
}

// paneLineCount returns how many lines a pane has.
func (v *mergeView) paneLineCount(k int) int {
	return len(v.paneLines(k))
}

// widestOf returns the longest line of lines, in columns.
func widestOf(lines []string) int {
	widest := 0
	for _, l := range lines {
		if n := columns(clipLine(l)); n > widest {
			widest = n
		}
	}
	return widest
}

// textXOf is where a pane's text starts, past the rail and the padding,
// and how wide its content is for its longest line.
func textXOf(f codeFont, widest int) float32 {
	return mergeTextX + textPadR + float32(widest)*f.charW
}

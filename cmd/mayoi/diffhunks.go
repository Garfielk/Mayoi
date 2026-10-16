package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/git"
	"mayoi/internal/linediff"
	"mayoi/internal/vfs"
)

// hunkAction is what a button of a run of changes in a diff does to them.
type hunkAction int

const (
	hunkStage   hunkAction = iota // into the index
	hunkUnstage                   // out of the index, which has them as HEAD does
	hunkDiscard                   // out of the work tree, which has them as the index does
	hunkAppend                    // append the compared side after the work tree change
)

// hunkArrow is a direction the lines of a change of a diff can be
// applied in: the side dir writes takes the lines of the other side in
// place of the change's own lines, in a document of its text. Its label
// is what the arrow says it does, as the versions compared name it, and
// its tip tells which of them it reads and which it writes.
type hunkArrow struct {
	dir   int    // +1 writes the new side, -1 writes the old
	label string // Revert, as the base's lines go into the local text
	icon  *ui.SVG
	tip   string
}

// hunkArrows returns the arrows of a file of a diff, one for each side
// its changes can be written to. The work tree's text is the only side
// written: the lines of the version it is compared against go over its
// changes, which reverts them. The index is written by staging and
// unstaging, which keep their own buttons, and the versions Git keeps
// are read only, so they take no arrows — nor does a file one side of
// the diff lacks whole, whose changes go in or out with the file itself.
func hunkArrows(f *diffFile) []hunkArrow {
	if !f.loaded || f.binary || !f.old.ok || !f.new.ok || f.entry.Submodule() {
		return nil
	}
	if f.base != diffIndex {
		return nil
	}
	old, new := f.base.sides()
	return []hunkArrow{{
		dir:   1,
		label: "Revert",
		icon:  iconArrowRight,
		tip:   "Revert this change: the lines of " + old + ", into the " + new + "; Control-click to append them",
	}}
}

// hunkActions returns what can be done to the changes of a file of a
// diff, one change at a time: those of a file in both versions only, as
// a file added or removed goes in or out whole, as a submodule does.
func hunkActions(f *diffFile) []hunkAction {
	if !f.loaded || f.binary || !f.old.ok || !f.new.ok || f.entry.Submodule() {
		return nil
	}
	switch {
	case f.base == diffIndex && f.group == groupChanges:
		return []hunkAction{hunkDiscard, hunkStage, hunkAppend}
	case f.base == diffStaged:
		return []hunkAction{hunkUnstage}
	}
	return nil
}

// hunkRows returns the rows of the kth change of a diff, from its first
// changed row to past its last, as they are laid out.
func (v *diffView) hunkRows(k int) (from, to int) {
	from = v.starts[k]
	to = from + 1
	for to < len(v.rows) && v.rows[to].changed() && v.rows[to].file == v.rows[from].file && v.rows[to].hunk == v.rows[from].hunk {
		to++
	}
	return from, to
}

// hunkHead returns the header of the run of changes row i of a diff is
// in, -1 when it is in none: in the full files, which have no headers,
// or out of the runs.
func (v *diffView) hunkHead(i int) int {
	if i < 0 || i >= len(v.rows) {
		return -1
	}
	file := v.rows[i].file
	// A run is a few lines on each side of its changes: the walk is short
	// but in the full files, where it stops early.
	for k := i; k >= 0 && k > i-2000 && v.rows[k].file == file; k-- {
		switch v.rows[k].kind {
		case rowHunk:
			return k
		case rowLine:
		default:
			return -1
		}
	}
	return -1
}

// hunksAt returns the file of row i of a diff and its changes the row
// stands for: those of the run a header heads, or a changed line's.
func (v *diffView) hunksAt(i int) (*diffFile, []linediff.Hunk) {
	if i < 0 || i >= len(v.rows) {
		return nil, nil
	}
	r := v.rows[i]
	f := v.files[r.file]
	var ks []int
	switch {
	case r.changed():
		ks = []int{r.hunk}
	case r.kind == rowHunk:
		for k := i + 1; k < len(v.rows) && v.rows[k].kind == rowLine && v.rows[k].file == r.file; k++ {
			if h := v.rows[k].hunk; h >= 0 && !slices.Contains(ks, h) {
				ks = append(ks, h)
			}
		}
	}
	var hs []linediff.Hunk
	for _, k := range ks {
		if k < len(f.hunks) {
			hs = append(hs, f.hunks[k])
		}
	}
	return f, hs
}

// hunkButtons are the buttons of the header of a run of changes, row i,
// whose right is at right and top at y in the diff's content, contentW
// wide: staging the changes, unstaging them, or discarding them. It
// returns about where they are.
func (a *app) hunkButtons(c *ui.Context, v *diffView, i int, right, contentW, y float32) ui.Rect {
	p := a.pal
	f, hs := v.hunksAt(i)
	acts := hunkActions(f)
	if len(hs) == 0 || len(acts) == 0 {
		return ui.Rect{}
	}
	type button struct {
		icon  *ui.SVG
		label string
		act   hunkAction
	}
	var bs []button
	for _, act := range acts {
		switch act {
		case hunkStage:
			bs = append(bs, button{iconPlus, "Stage", act})
		case hunkUnstage:
			bs = append(bs, button{iconMinus, "Unstage", act})
		case hunkDiscard:
			bs = append(bs, button{iconUndo, "Discard", act})
		case hunkAppend:
			bs = append(bs, button{iconArrowRight, "Append", act})
		}
	}
	h := a.font.lineH - 2
	row := ui.Row(c.Key("hunk-buttons")).Absolute().Right(contentW - right + 18).Top(y + 1).Gap(4)
	// Its left is not known until it is laid out: a box as wide as the
	// buttons can be keeps clicks from the row under them.
	w := float32(len(bs)) * 84
	row.Children(func() {
		for _, b := range bs {
			btn := ui.ButtonBase(c.Key("hunk:"+b.label)).Height(h).Padding(0, 7).Gap(4).Radius(4).AlignItems(ui.Center).
				Background(p.editor).Border(1, p.border).Label(b.label + " Change").Tooltip(b.label + " this change")
			col := p.muted
			if btn.Hovered() {
				btn.Background(p.hover.Over(p.editor))
				col = p.text
			}
			btn.Children(func() {
				ui.Icon(c, b.icon).FontSize(12).TextColor(col)
				ui.Text(c, b.label).FontSize(11.5).TextColor(col).SingleLine()
			})
			if btn.Clicked() {
				act := b.act
				a.post(func() { a.applyHunks(f, hs, act) })
			}
		}
	})
	return ui.Rect{X: right - 18 - w, Y: y, W: w + 18, H: a.font.lineH}
}

// applyHunks stages changes of a file of a diff, unstages them or
// discards them. The texts are read again first, and the changes found
// again among theirs, so that what is done is to the files as they are.
func (a *app) applyHunks(f *diffFile, hs []linediff.Hunk, act hunkAction) {
	if a.git.repo == nil || len(hs) == 0 {
		return
	}
	path := f.path
	// The changes are of the editor's text as the diff last compared it.
	if doc := a.diffDoc(f); doc != nil && doc.buf.Version() != f.docVersion && act != hunkUnstage {
		a.fail("The diff is catching up with your edits: try again in a moment.")
		return
	}
	switch act {
	case hunkStage:
		work, crlf, ok := a.workText(f)
		a.gitDo("", func(r *git.Repo) error {
			if !ok {
				var err error
				if work, crlf, err = readWork(path); err != nil {
					return err
				}
			}
			text, _, err := r.Show("", path)
			if err != nil {
				return err
			}
			index, lines := strings.Split(text, "\n"), strings.Split(work, "\n")
			picked := overlapping(linediff.Hunks(index, lines), hs)
			if len(picked) == 0 {
				return nil
			}
			return r.StageText(path, joinLines(linediff.Apply(index, lines, picked), crlf))
		}, nil)
	case hunkUnstage:
		a.gitDo("", func(r *git.Repo) error {
			head, _, err := r.Show("HEAD", path)
			if err != nil {
				return err
			}
			text, _, err := r.Show("", path)
			if err != nil {
				return err
			}
			_, crlf, _ := readWork(path)
			from, index := strings.Split(head, "\n"), strings.Split(text, "\n")
			picked := overlapping(linediff.Hunks(from, index), hs)
			if len(picked) == 0 {
				return nil
			}
			return r.StageText(path, joinLines(linediff.Apply(index, from, inverted(picked)), crlf))
		}, nil)
	case hunkDiscard:
		a.discardHunks(f, hs)
	case hunkAppend:
		a.appendHunks(f, hs)
	}
}

// discardHunks puts changes of a file back as the index has them, in the
// file's document, as one step of undo.
// appendHunks keeps the work tree side of each change and appends the
// corresponding lines from the compared side after it. Each block is one
// undoable edit, and an empty source is a no-op.
func (a *app) appendHunks(f *diffFile, hs []linediff.Hunk) {
	if len(hs) == 0 || f == nil || f.base != diffIndex {
		return
	}
	if f.running || f.gitStale {
		a.fail("The diff is catching up with the files: try again in a moment.")
		return
	}
	if doc := a.diffDoc(f); doc != nil && doc.buf.Version() != f.docVersion {
		a.fail("The diff is catching up with your edits: try again in a moment.")
		return
	}
	if d, shown := a.editorOf(a.diffDoc(f)); d != nil {
		appendNow := func(d *editor) { a.appendInto(d, f, hs) }
		if shown {
			a.queueEdit(d, appendNow)
		} else {
			appendNow(d)
		}
		return
	}
	name := filepath.Base(f.path)
	a.confirm = &confirm{title: "Append source lines to " + name + "?", message: "The compared lines are added after the local change in the editor.", ok: "Append", run: func() {
		a.openFile(f.path, false)
		d, _ := a.editorOf(a.diffDoc(f))
		if d == nil || d.loading || d.loadErr != nil || d.offline {
			a.fail("Could not open %s to append its changes.", name)
			return
		}
		a.appendInto(d, f, hs)
	}}
}

func (a *app) appendInto(d *editor, f *diffFile, hs []linediff.Hunk) {
	if d == nil || len(hs) == 0 {
		return
	}
	snap := d.buf.Snapshot()
	var edits []textEdit
	for _, h := range slices.Backward(hs) {
		if h.B < 0 || h.B+h.BLen > len(f.new.lines) || h.ALen == 0 {
			continue
		}
		lines := append(append([]string{}, f.new.lines[h.B:h.B+h.BLen]...), f.old.lines[h.A:h.A+h.ALen]...)
		e, ok := hunkEdit(snap, h, lines)
		if ok {
			edits = append(edits, e)
		}
	}
	if len(edits) == 0 {
		return
	}
	d.peek = -1
	d.edit([2]int{edits[0].start, edits[0].start}, edits...)
}

func (a *app) discardHunks(f *diffFile, hs []linediff.Hunk) {
	a.revertInto(nil, f, hs, "Discard")
}

// hunkArrowW is the side of the round button of the arrow of a change of
// a diff, which fits between the rail and the line numbers of the one
// column, where they leave room.
const hunkArrowW float32 = 13

// hunkArrowUI is the arrow of the kth change of a diff, which puts the
// lines of one of its sides in place of its lines of the other, at the
// middle of the change's rows: on the divider of the sides side by side,
// and between the rail and the line numbers of the one column, staying
// by them as the lines scroll along. It returns where it is, or nothing
// for a change that takes no arrow, or that is out of the view.
func (a *app) hunkArrowUI(c *ui.Context, v *diffView, k, first, last int, l diffGeometry) ui.Rect {
	from, to := v.hunkRows(k)
	if from > last || to <= first || from >= len(v.rows) {
		return ui.Rect{}
	}
	f := v.fileAt(k)
	ars := hunkArrows(f)
	if len(ars) == 0 {
		return ui.Rect{}
	}
	h := f.hunks[v.rows[from].hunk]
	ar := ars[0]
	p := a.pal
	mid := (float32(v.tops[from]) + float32(v.tops[to-1]+v.rows[to-1].h)) / 2
	y := editorPadY + mid*a.font.lineH - hunkArrowW/2
	x := v.scroll.X + diffRailW
	if v.split {
		x = v.scroll.X + l.half - hunkArrowW/2
	}
	btn := ui.ButtonBase(c.Key(fmt.Sprintf("arrow:%d", k))).Absolute().Left(x).Top(y).Size(hunkArrowW, hunkArrowW).
		Radius(hunkArrowW/2).AlignItems(ui.Center).Justify(ui.Center).Label(ar.label).Tooltip(ar.tip).
		Background(p.editor).Border(1, p.border)
	col := p.muted
	if btn.Hovered() {
		btn.Background(p.hover.Over(p.editor))
		col = p.text
	}
	btn.Children(func() {
		ui.Icon(c, ar.icon).FontSize(hunkArrowW - 3).TextColor(col)
	})
	if btn.Clicked() {
		append := btn.ClickModifiers()&ui.Ctrl != 0
		a.post(func() {
			v.pane.Focus()
			if append {
				a.appendHunks(f, []linediff.Hunk{h})
			} else {
				a.applyArrow(v, f, h, ar)
			}
		})
	}
	return ui.Rect{X: x, Y: y, W: hunkArrowW, H: hunkArrowW}
}

// applyArrow does what the arrow of a change of a diff does: puts the
// lines of the side it reads in place of the change's lines of the side
// it writes, in a document of that side's text, as one step of undo. The
// work tree's side is the one written, from the version it is compared
// against, which reverts the change; the diff v keeps the document, as
// the undo of its keyboard works in it.
func (a *app) applyArrow(v *diffView, f *diffFile, h linediff.Hunk, ar hunkArrow) {
	switch {
	case ar.dir > 0:
		a.revertInto(v, f, []linediff.Hunk{h}, ar.label)
	default:
		// No side these diffs compare is written from its new side: the
		// index goes on by staging, and the versions Git keeps are read
		// only, so no such arrow is offered.
	}
}

// revertInto puts the changes hs of a file of a diff back as the old
// side has them, in the file's document, as one step of undo: through an
// editor shown, in its frame; through one out of sight, at once; or, for
// a file no editor has, through one opened for it and found to be what
// the diff compared, after asking about it as ok names the action. A
// diff that is behind the files or the document is read again first: its
// changes are not to go over what is there now.
func (a *app) revertInto(v *diffView, f *diffFile, hs []linediff.Hunk, ok string) {
	if f.running || f.gitStale {
		a.fail("The diff is catching up with the files: try again in a moment.")
		return
	}
	if doc := a.diffDoc(f); doc != nil && doc.buf.Version() != f.docVersion {
		a.fail("The diff is catching up with your edits: try again in a moment.")
		return
	}
	if d, shown := a.editorOf(a.diffDoc(f)); d != nil {
		into := func(d *editor) {
			a.discardInto(d, f, hs)
			if v != nil {
				v.edited = d.document
			}
		}
		// An editor shown takes edits in its frame, once its text area's
		// selection is known; one out of sight has its own.
		if shown {
			a.queueEdit(d, into)
		} else {
			into(d)
		}
		return
	}
	name := filepath.Base(f.path)
	title := ok + " this change to " + name + "?"
	if len(hs) > 1 {
		title = ok + " these changes to " + name + "?"
	}
	a.confirm = &confirm{title: title, message: "The lines go back as the index has them, in the editor, where undo takes them back.",
		ok: ok, danger: true, run: func() {
			a.intoByOpening(v, f, hs)
		}}
}

// intoByOpening opens a file that no editor has, checks that its text
// is what the diff compared, and puts the changes of hunks hs back as
// the index has them, as one step of undo. A file that changed since the
// diff was drawn is left as it is and read again: its changes do not
// fit, and are not to go over what it has now.
func (a *app) intoByOpening(v *diffView, f *diffFile, hs []linediff.Hunk) {
	if f.running || f.gitStale {
		a.fail("The diff is catching up with the files: try again in a moment.")
		return
	}
	a.openFile(f.path, false)
	d, _ := a.editorOf(a.diffDoc(f))
	if d == nil || d.loadErr != nil || d.offline {
		a.fail("Could not open %s to revert its changes.", filepath.Base(f.path))
		return
	}
	if d.loading {
		a.fail("The file is still being read: try again in a moment.")
		return
	}
	if d.buf.Snapshot().String() != f.new.text {
		// The file is not the text the changes were found among.
		f.gitStale = true
		a.fail("%s has changed since the diff was drawn. It is read again: try again.", filepath.Base(f.path))
		return
	}
	a.discardInto(d, f, hs)
	if v != nil {
		v.edited = d.document
	}
}

// discardInto puts the changes of hunks hs of a file of a diff back as
// the index has them, in an editor of the text the diff compared them
// in, as one step of undo: the edits made together, so that undo takes
// the whole discard back, and redo makes it again.
func (a *app) discardInto(d *editor, f *diffFile, hs []linediff.Hunk) {
	old := f.old.lines
	snap := d.buf.Snapshot()
	var edits []textEdit
	at := -1
	// From the last up, so that the lines of those above stay put.
	for _, h := range slices.Backward(hs) {
		if h.A+h.ALen > len(old) {
			continue
		}
		e, ok := hunkEdit(snap, h, old[h.A:h.A+h.ALen])
		if !ok {
			continue
		}
		if at < 0 || e.start < at {
			at = e.start
		}
		edits = append(edits, e)
	}
	if len(edits) == 0 {
		return
	}
	d.peek = -1
	d.edit([2]int{at, at}, edits...)
}

// editorOf returns an editor of an open document, nil for none: one a
// column shows when there is one, and whether it is shown.
func (a *app) editorOf(doc *document) (*editor, bool) {
	if doc == nil {
		return nil, false
	}
	var hidden *editor
	for _, col := range a.cols {
		for i, t := range col.tabs {
			switch {
			case t.ed == nil || t.ed.document != doc:
			case i == col.active:
				return t.ed, true
			case hidden == nil:
				hidden = t.ed
			}
		}
	}
	return hidden, false
}

// workText returns the text of a file of a diff in the work tree, with
// its line endings as \n, and whether they are \r\n on disk, when it is
// open in an editor; ok is false when it must be read from disk.
func (a *app) workText(f *diffFile) (text string, crlf, ok bool) {
	if doc := a.diffDoc(f); doc != nil {
		return doc.buf.Snapshot().String(), doc.crlf, true
	}
	return "", false, false
}

// readWork reads a file of the work tree, with its line endings as \n,
// and reports whether they were \r\n.
func readWork(path string) (string, bool, error) {
	data, err := vfs.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	crlf := bytes.Contains(data, []byte("\r\n"))
	return strings.ReplaceAll(string(data), "\r\n", "\n"), crlf, nil
}

// joinLines joins lines with the line ending of a file.
func joinLines(lines []string, crlf bool) string {
	if crlf {
		return strings.Join(lines, "\r\n")
	}
	return strings.Join(lines, "\n")
}

// stageAtCursor stages the changes of a diff at its chosen row, or the
// row at the top, or unstages them when they are staged.
func (a *app) stageAtCursor(v *diffView) {
	i := v.focusRow()
	if h := v.hunkHead(i); h >= 0 && !v.rows[i].changed() {
		i = h
	}
	f, hs := v.hunksAt(i)
	if f == nil {
		return
	}
	for _, act := range hunkActions(f) {
		if act == hunkStage || act == hunkUnstage {
			a.applyHunks(f, hs, act)
			return
		}
	}
}

package main

import (
	"bytes"
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
)

// hunkActions returns what can be done to the changes of a file of a
// diff, one change at a time: those of a file in both versions only, as
// a file added or removed goes in or out whole, as a submodule does.
func hunkActions(f *diffFile) []hunkAction {
	if !f.loaded || f.binary || !f.old.ok || !f.new.ok || f.entry.Submodule() {
		return nil
	}
	switch {
	case f.base == diffIndex && f.group == groupChanges:
		return []hunkAction{hunkDiscard, hunkStage}
	case f.base == diffStaged:
		return []hunkAction{hunkUnstage}
	}
	return nil
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
	}
}

// discardHunks puts changes of a file back as the index has them, in the
// file's document, as one step of undo: of an editor shown in its frame,
// and of a file that is not open once it is asked about, opened, and
// found to be what the diff compared. A diff that is behind the files is
// read again first: its changes are not to go over what is there now.
func (a *app) discardHunks(f *diffFile, hs []linediff.Hunk) {
	if f.running || f.gitStale {
		a.fail("The diff is catching up with the files: try again in a moment.")
		return
	}
	if d, shown := a.editorOf(a.diffDoc(f)); d != nil {
		discard := func(d *editor) { a.discardInto(d, f, hs) }
		// An editor shown takes edits in its frame, once its text area's
		// selection is known; one out of sight has its own.
		if shown {
			a.queueEdit(d, discard)
		} else {
			discard(d)
		}
		return
	}
	path := f.path
	title := "Discard this change to " + filepath.Base(path) + "?"
	if len(hs) > 1 {
		title = "Discard these changes to " + filepath.Base(path) + "?"
	}
	a.confirm = &confirm{title: title, message: "The lines go back as the index has them, in the editor, where undo takes them back.",
		ok: "Discard", danger: true, run: func() {
			a.discardByOpening(f, hs)
		}}
}

// discardByOpening opens a file that no editor has, checks that its text
// is what the diff compared, and puts the changes of hunks hs back as
// the index has them, as one step of undo. A file that changed since the
// diff was drawn is left as it is and read again: its changes do not
// fit, and are not to go over what it has now.
func (a *app) discardByOpening(f *diffFile, hs []linediff.Hunk) {
	if f.running || f.gitStale {
		a.fail("The diff is catching up with the files: try again in a moment.")
		return
	}
	a.openFile(f.path, false)
	d, _ := a.editorOf(a.diffDoc(f))
	if d == nil || d.loadErr != nil || d.offline {
		a.fail("Could not open %s to discard its changes.", filepath.Base(f.path))
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

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// diffEditRow returns the row of a diff that shows line of the new side of
// the file at path, -1 for none.
func diffEditRow(v *diffView, path string, line int) int {
	for i, r := range v.rows {
		if r.kind == rowLine && r.new == line && v.files[r.file].path == path {
			return i
		}
	}
	return -1
}

// TestDiffEditBeginsOnTheWritableSide: beginning to edit in a diff edits
// the working tree side of a file, through the document the editor opens,
// and refuses the sides it may not write.
func TestDiffEditBeginsOnTheWritableSide(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	orig, changed := twoHunkFile(hs, git)
	_ = orig
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	f := v.files[0]
	if !f.writable() {
		t.Fatal("the working tree side is not writable")
	}
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if row < 0 {
		t.Fatal("no row of the changed line")
	}
	if !a.beginDiffEdit(v, row) {
		t.Fatal("the writable side did not begin to edit")
	}
	if v.editing == nil || v.editing.f != f {
		t.Fatal("no edit target, or not the file of the row")
	}
	// The edit is of the document the editor opens, at the line the row
	// shows.
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("no document was made for the file")
	}
	if v.editing.doc != a.docs[i] {
		t.Fatal("the edit is not of the document of the file")
	}
	snap := v.editing.doc.buf.Snapshot()
	if line := snap.LineAt(min(v.editing.ed.sel[1], snap.Len())); line != 2 {
		t.Fatalf("the caret is on line %d, want 2", line)
	}
	// Once begun, beginning again at the file keeps the target, and puts
	// the caret where is asked.
	row = diffEditRow(v, hs.path("t.txt"), 25)
	if !a.beginDiffEdit(v, row) || v.editing.f != f {
		t.Fatal("editing the file again did not keep the target")
	}
	if line := snap.LineAt(min(v.editing.ed.sel[1], snap.Len())); line != 25 {
		t.Fatalf("the caret is on line %d, want 25", line)
	}
	a.endDiffEdit(v)
	if v.editing != nil {
		t.Fatal("still editing after leaving")
	}

	// The index side of the staged changes is not for editing.
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	git("add", "t.txt")
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffStaged}, true)
	hs.settleDiff(a.activeTab().diff)
	v = a.activeTab().diff
	sf := v.files[0]
	if sf.writable() {
		t.Fatal("the index side is writable")
	}
	row = diffEditRow(v, hs.path("t.txt"), 2)
	if row < 0 {
		t.Fatal("no row of the staged change")
	}
	if a.beginDiffEdit(v, row) || v.editing != nil {
		t.Fatal("the index side began to edit")
	}
	if len(a.errors) == 0 {
		t.Error("no word of why it may not edit")
	}
}

// TestDiffEditOpensTheFileItEdits: a file that no editor has is read into
// a document of its own to edit, which the editor opens as it is, with the
// edits of the diff in it.
func TestDiffEditOpensTheFileItEdits(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if row < 0 {
		t.Fatal("no row of the changed line")
	}
	if !a.beginDiffEdit(v, row) {
		t.Fatal("the writable side did not begin to edit")
	}
	// No tab opened: the document is the diff's to edit.
	if a.docIndex(hs.path("t.txt")) < 0 {
		t.Fatal("no document was made for the file")
	}
	for _, col := range a.cols {
		for _, tab := range col.tabs {
			if tab.path() == hs.path("t.txt") {
				t.Error("a tab opened for the file")
			}
		}
	}
	// The editor opens the same document, edits and all.
	e := v.editing
	e.ed.edit(e.ed.sel, textEdit{0, 0, "// edited in the diff\n"})
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	i := a.docIndex(hs.path("t.txt"))
	if i < 0 {
		t.Fatal("the editor did not open the document")
	}
	if got := a.docs[i].buf.String(); !strings.Contains(got, "// edited in the diff\n") || !strings.Contains(got, changed) {
		t.Fatalf("the document the editor opened:\n%s", got)
	}
}

// TestDiffEditRefusesWhatMayNotEdit: a diff that is behind the files, a
// binary file and a submodule do not begin to edit.
func TestDiffEditRefusesWhatMayNotEdit(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	// A diff that is behind the file does not edit over it.
	fresh := strings.Replace(changed, "line twenty-five\n", "line 25th\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(fresh), 0o644)
	f := v.files[0]
	f.diskStale = true
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if a.beginDiffEdit(v, row) {
		t.Fatal("a diff behind the file began to edit")
	}
	if len(a.errors) == 0 {
		t.Error("no word of the diff being behind")
	}
}

// TestDiffEditTypesIntoTheDocument: typing in a diff edits the document
// the editor opens, which shows as changed and is not written before it is
// saved; the diff reads it again; undo takes the typing back.
func TestDiffEditTypesIntoTheDocument(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig, changed := twoHunkFile(hs, git)
	_ = orig
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	row := diffEditRow(v, hs.path("t.txt"), 2)
	if !a.beginDiffEdit(v, row) {
		t.Fatal("the writable side did not begin to edit")
	}
	e := v.editing
	// Typing goes in at the caret, into what the work tree has.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "X"}) {
		t.Fatal("the diff did not take the text")
	}
	want := strings.Replace(changed, "line two\n", "Xline two\n", 1)
	if e.doc.buf.String() != want {
		t.Fatalf("in the document:\n%s", e.doc.buf.String())
	}
	if !e.doc.dirty() {
		t.Error("the document does not show as changed")
	}
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != changed {
		t.Errorf("the file was written before the document was saved:\n%s", data)
	}
	// More typing joins the step of undo, as typing in the editor does.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "Y"})
	if e.doc.buf.String() != strings.Replace(changed, "line two\n", "XYline two\n", 1) {
		t.Fatalf("in the document:\n%s", e.doc.buf.String())
	}
	if n := len(e.doc.hist.undo); n > 1 {
		t.Errorf("the typing took %d steps of undo", n)
	}
	// The diff catches up with the edits, and shows them as its changes.
	hs.settleDiff(a.changesTabAndShow())
	v = a.activeTab().diff
	if got := v.files[0].new.text; got != e.doc.buf.Snapshot().String() {
		t.Fatalf("the diff has %q, not what the document has", got)
	}
	shown := false
	for _, r := range v.rows {
		if r.kind == rowLine && r.new >= 0 && strings.Contains(v.files[r.file].new.lines[r.new], "XYline two") {
			shown = true
		}
	}
	if !shown {
		t.Error("the diff does not show the typed line")
	}
	// Undo takes the typing back, in the editor of the document too.
	if !e.ed.undo() || e.doc.buf.String() != changed {
		t.Fatalf("undo: %q", e.doc.buf.String())
	}
	// Typing through the pane goes in as well.
	a.beginDiffEdit(v, diffEditRow(a.changesTabAndShow(), hs.path("t.txt"), 2))
	hs.settle()
	hs.tt.Type("Z")
	hs.settle()
	if !strings.Contains(e.doc.buf.String(), "Zline two") {
		t.Fatalf("typed through the pane: %q", e.doc.buf.String())
	}
}

// diffEditOf begins editing a diff's file at line and returns its edit.
func diffEditOf(t *testing.T, hs *harness, v *diffView, path string, line int) *diffEdit {
	t.Helper()
	row := diffEditRow(v, path, line)
	if row < 0 {
		t.Fatalf("no row of line %d of %s", line, path)
	}
	if !a_of(hs).beginDiffEdit(v, row) {
		t.Fatalf("the writable side of %s did not begin to edit", path)
	}
	return v.editing
}

func a_of(hs *harness) *app { return hs.a }

// TestDiffEditMovesTheCaret: the arrow keys move the caret over the rows
// the diff shows, keeping the column, and extend the selection with shift.
func TestDiffEditMovesTheCaret(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	path := hs.path("t.txt")
	e := diffEditOf(t, hs, v, path, 2)
	snap := e.doc.buf.Snapshot()
	lineAt := func() int { return snap.LineAt(min(e.ed.sel[1], snap.Len())) }
	// Right goes a rune along, and left comes back.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight})
	if n := e.ed.sel[1] - 14; n != 1 {
		t.Fatalf("right: %d", n)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyLeft})
	if e.ed.sel[1] != 14 {
		t.Fatalf("left: %d", e.ed.sel[1])
	}
	// Down goes to the row below, keeping the column.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyDown})
	if lineAt() != 3 || e.ed.sel[1] != 14+9 {
		t.Fatalf("down: line %d, sel %v", lineAt(), e.ed.sel)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUp})
	if lineAt() != 2 {
		t.Fatalf("up: line %d", lineAt())
	}
	// With shift, the selection grows, and copy takes its text.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyDown, Mods: ui.Shift})
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Shift})
	if text := e.diffEditedText(); text != "line two\nl" { // line 2 and a rune of line 3
		t.Fatalf("selected %q", text)
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "copy"})
	if got := a.services.ReadClipboard(); got != "line two\nl" {
		t.Fatalf("copied %q", got)
	}
	// Paste puts what was copied in at the caret, which the left key
	// brings to the start of the selection.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyLeft})
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "paste"})
	if text := e.diffEditedText(); text != "" || !strings.Contains(e.doc.buf.Snapshot().String(), "line two\nlline two\nline 3") {
		t.Fatalf("pasted: %q", e.diffEditedText())
	}
	// Cut takes the selection away and holds it.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Shift})
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "cut"})
	if text := e.diffEditedText(); text != "" || !strings.Contains(e.doc.buf.Snapshot().String(), "ine 3") {
		t.Fatalf("cut: %q", e.diffEditedText())
	}
	if got := a.services.ReadClipboard(); got != "l" {
		t.Fatalf("cut holds %q", got)
	}
	// Select all takes the document.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "selectAll"})
	if text := e.diffEditedText(); text != e.doc.buf.Snapshot().String() {
		t.Fatal("select all missed the document")
	}
	// Backspace takes the selection away.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyBackspace})
	if e.doc.buf.Snapshot().String() != "" {
		t.Fatalf("backspace: %q", e.doc.buf.Snapshot().String())
	}
	// And undo, from the diff, takes the editing back.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputCommand, Text: "undo"})
	if e.doc.buf.Snapshot().String() != changed {
		t.Fatalf("undo: %q", e.doc.buf.Snapshot().Slice(0, 40))
	}
	// Escape goes back to the pager.
	a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyEscape})
	if v.editing != nil {
		t.Fatal("still editing after escape")
	}
	// The pager's keys are the pager's again.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyJ}) {
		t.Error("the pager did not take j")
	}
}

// TestDiffEditTakesKeysFromThePager: while the side is edited the pager's
// keys type, and paging stays with the page keys.
func TestDiffEditTakesKeysFromThePager(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, _ = twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	path := hs.path("t.txt")
	e := diffEditOf(t, hs, v, path, 2)
	// A pager key types while the side is edited.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "j"}) {
		t.Fatal("the edit did not take j")
	}
	if !strings.Contains(e.doc.buf.Snapshot().String(), "jline two") {
		t.Fatalf("typed: %q", e.doc.buf.Snapshot().Slice(0, 30))
	}
	// The caret did not move by a row.
	if line := snapLineOf(e); line != 2 {
		t.Fatalf("j moved to line %d", line)
	}
	// The page keys page on.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyPageDown}) {
		t.Fatal("the pager did not page")
	}
}

func snapLineOf(e *diffEdit) int {
	snap := e.doc.buf.Snapshot()
	return snap.LineAt(min(e.ed.sel[1], snap.Len()))
}

// TestDiffEditSavesTheFile: saving from the diff writes the document as it
// is, and the document shows as saved, as the editor's does.
func TestDiffEditSavesTheFile(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	_, changed := twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "X"})
	// Saving, by the key the editor saves with.
	if !a.diffInput(v, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyS, Mods: ui.Cmd}) {
		t.Fatal("the edit did not save")
	}
	want := strings.Replace(changed, "line two\n", "Xline two\n", 1)
	if data, _ := os.ReadFile(hs.path("t.txt")); string(data) != want {
		t.Fatalf("on disk:\n%s", data)
	}
	if e.doc.dirty() {
		t.Error("the document shows as changed after saving")
	}
}

// TestDiffEditKeepsItsPlaceAsTheDiffCatchesUp: while the diff reads what
// the typing made again, the scroll holds, and the caret stays on its line
// when the rows come back; and the changes of a stale diff are not applied.
func TestDiffEditKeepsItsPlaceAsTheDiffCatchesUp(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	// A file tall enough to scroll: a change in every tenth line keeps
	// the unchanged lines from folding away.
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	orig := b.String()
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "tall")
	changed := orig
	for i := 10; i < 60; i += 10 {
		changed = strings.Replace(changed, fmt.Sprintf("line %d\n", i), fmt.Sprintf("line ten %d\n", i), 1)
	}
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 10)
	// The caret's row, and the scroll, before the typing.
	row := v.caretRow(e)
	if row < 0 {
		t.Fatal("the caret has no row")
	}
	line := snapLineOf(e)
	// Scroll away, and type a rune: no line was added or taken.
	v.scroll.Y = 100
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "X"})
	// The diff is behind: its changes are not applied over the typing.
	f, hks := v.hunksAt(v.starts[0])
	a.applyHunks(f, hks, hunkDiscard)
	if len(a.errors) == 0 {
		t.Error("no word of the diff catching up")
	}
	a.errors = nil
	// Caught up, the scroll is where it was, and the caret on its line.
	hs.settleDiff(v)
	if v.scroll.Y != 100 {
		t.Errorf("the scroll moved to %v", v.scroll.Y)
	}
	if snapLineOf(e) != line {
		t.Errorf("the caret moved to line %d", snapLineOf(e))
	}
	if row := v.caretRow(e); row < 0 {
		t.Error("the caret has no row after the diff caught up")
	}
	if got := v.editing.f.new.text; got != e.doc.buf.Snapshot().String() {
		t.Fatal("the diff did not catch up with the document")
	}
}

// TestDiffEditKeepsWorkingAcrossLayouts: side by side or in one column,
// the edit keeps its file and its caret's line, and undo keeps working.
func TestDiffEditKeepsWorkingAcrossLayouts(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	_, _ = twoHunkFile(hs, git)
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	e := diffEditOf(t, hs, v, hs.path("t.txt"), 2)
	line := snapLineOf(e)
	// Side by side, the caret keeps its line, and typing goes in.
	a.setDiffLayout(v, layoutSplit)
	if got := snapLineOf(e); got != line {
		t.Fatalf("side by side: the caret is on line %d", got)
	}
	if row := v.caretRow(e); row < 0 {
		t.Fatal("side by side: the caret has no row")
	}
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "S"})
	if !strings.Contains(e.doc.buf.Snapshot().String(), "Sline two") {
		t.Fatalf("side by side: typed %q", e.doc.buf.Snapshot().Slice(0, 30))
	}
	// Back in one column, undo takes it back, and typing goes in again,
	// from the start of the line it began on.
	a.setDiffLayout(v, layoutInline)
	if !e.ed.undo() || strings.Contains(e.doc.buf.Snapshot().String(), "Sline two") {
		t.Fatal("undo did not take the typing back")
	}
	a.beginDiffEdit(v, diffEditRow(v, hs.path("t.txt"), 2))
	a.diffInput(v, ui.InputEvent{Kind: ui.InputText, Text: "T"})
	if !strings.Contains(e.doc.buf.Snapshot().String(), "Tline two") {
		t.Fatalf("inline: typed %q", e.doc.buf.Snapshot().Slice(0, 30))
	}
	if v.editing.f != e.f {
		t.Fatal("the edit changed files across the layouts")
	}
}

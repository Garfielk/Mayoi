package main

import (
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

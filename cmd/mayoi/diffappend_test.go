package main

import (
	"os"
	"strings"
	"testing"
)

func TestAppendSourceLinesKeepsLocalChange(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	a := hs.a
	a.settings.AutoSave = false
	orig := "one\ntwo\nthree\n"
	os.WriteFile(hs.path("t.txt"), []byte(orig), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	changed := strings.Replace(orig, "two\n", "local\n", 1)
	os.WriteFile(hs.path("t.txt"), []byte(changed), 0o644)
	a.openFile(hs.path("t.txt"), false)
	hs.settle()
	hs.settleGit()
	a.showChanges(diffTarget{path: hs.path("t.txt"), base: diffIndex}, true)
	hs.settleDiff(a.activeTab().diff)
	v := a.activeTab().diff
	f, hs0 := v.hunksAt(v.starts[0])
	if len(hs0) != 1 {
		t.Fatalf("hunks: %v", hs0)
	}
	a.appendHunks(f, hs0)
	hs.settle()
	got := a.docs[a.docIndex(hs.path("t.txt"))].buf.String()
	if want := "one\nlocal\ntwo\nthree\n"; got != want {
		t.Fatalf("append result %q, want %q", got, want)
	}
	a.docs[a.docIndex(hs.path("t.txt"))].views[0].undo()
	if got := a.docs[a.docIndex(hs.path("t.txt"))].buf.String(); got != changed {
		t.Fatalf("undo result %q, want %q", got, changed)
	}
}

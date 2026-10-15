package main

import (
	"bytes"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	gitpkg "mayoi/internal/git"
)

func TestMergeRegions(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6"}
	// Both sides changed line 3: one conflict, at the side's own lines.
	rs := mergeRegions(base,
		[]string{"l1", "l2", "OURS", "l4", "l5", "l6"},
		[]string{"l1", "l2", "SIDE", "l4", "l5", "l6"})
	if len(rs) != 1 {
		t.Fatalf("got %d regions, want 1", len(rs))
	}
	r := rs[0]
	if r.baseLo != 2 || r.baseHi != 3 {
		t.Errorf("base [%d,%d), want [2,3)", r.baseLo, r.baseHi)
	}
	if r.oursLo != 2 || r.oursHi != 3 {
		t.Errorf("ours [%d,%d), want [2,3)", r.oursLo, r.oursHi)
	}
	if r.theirsLo != 2 || r.theirsHi != 3 {
		t.Errorf("theirs [%d,%d), want [2,3)", r.theirsLo, r.theirsHi)
	}
}

// regionsOf is what the tests read of the regions: their ranges, as
// "base 2:3 ours 2:3 theirs 2:3".
func regionsOf(rs []*mergeRegion) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.name()
	}
	return out
}

func TestMergeRegionsJoin(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6"}
	// Hunks that touch in the base are one conflict, as Git merges them:
	// ours changed line 3, theirs lines 3 and 4. Ours has the base's line
	// 4 as it is, so its side of the region is two lines too.
	rs := mergeRegions(base,
		[]string{"l1", "l2", "OURS", "l4", "l5", "l6"},
		[]string{"l1", "l2", "SIDE", "SIDE4", "l5", "l6"})
	want := []string{"base [2,4) ours [2,4) theirs [2,4)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeRegionsApart(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"}
	// Changes a line apart are no conflict: only the base both sides
	// changed makes one.
	rs := mergeRegions(base,
		[]string{"l1", "OURS", "l3", "l4", "l5", "l6", "l7", "l8"},
		[]string{"l1", "l2", "l3", "l4", "SIDE", "l6", "l7", "l8"})
	if len(rs) != 0 {
		t.Errorf("got %v, want none", regionsOf(rs))
	}
}

func TestMergeRegionsInsert(t *testing.T) {
	base := []string{"l1", "l2", "l3"}
	// Both sides inserted at the same place: a conflict of empty base.
	rs := mergeRegions(base,
		[]string{"l1", "OURS", "OURS2", "l2", "l3"},
		[]string{"l1", "SIDE", "l2", "l3"})
	want := []string{"base [1,1) ours [1,3) theirs [1,2)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeRegionsDelete(t *testing.T) {
	base := []string{"l1", "l2", "l3", "l4", "l5", "l6"}
	// Ours deleted the lines theirs changed: the conflict keeps both, and
	// the side's range is empty where it has no lines, the base's lines it
	// left in theirs' place.
	rs := mergeRegions(base,
		[]string{"l1", "l6"},
		[]string{"l1", "l2", "SIDE", "SIDE4", "l5", "l6"})
	want := []string{"base [1,5) ours [1,1) theirs [1,5)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeRegionsTwo(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	// Two conflicts apart: both sides changed b, c and h, i. The changes
	// of ours at b, c moved its h, i down a line.
	rs := mergeRegions(base,
		[]string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"},
		[]string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"})
	want := []string{"base [1,3) ours [1,3) theirs [1,3)", "base [7,9) ours [7,9) theirs [7,9)"}
	if got := regionsOf(rs); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// syncOf works the regions of a three-way text out for a result, and
// returns how each stands there: the lines it has of the region, as "-",
// "L", "R" or "B" for a side taken, both, or the base's.
func syncOf(base, ours, theirs, result []string) []string {
	v := &mergeView{baseLines: base, oursLines: ours, theirsLines: theirs}
	v.regions = mergeRegions(base, ours, theirs)
	v.sync(result)
	out := make([]string, len(v.regions))
	for i, r := range v.regions {
		s := "B"
		switch {
		case r.takenL && r.takenR:
			s = "LR"
		case r.takenL:
			s = "L"
		case r.takenR:
			s = "R"
		}
		content := ""
		if r.atLo >= 0 && r.atHi >= r.atLo && r.atHi <= len(result) {
			content = strings.Join(result[r.atLo:r.atHi], "|")
		}
		out[i] = s + " " + ita(r.atLo, r.atHi) + " " + content
	}
	return out
}

func TestSyncRegionsBase(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	got := syncOf(base, ours, theirs, base)
	want := []string{"B [1,3) b|c", "B [7,9) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSyncRegionsTaken(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	// The first region taken ours: it is its lines, the second stays the
	// base's, pushed down by the first's growth.
	got := syncOf(base, ours, theirs, []string{"a", "O", "O2", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	want := []string{"L [1,3) O|O2", "B [7,9) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Both regions taken theirs.
	got = syncOf(base, ours, theirs, []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"})
	want = []string{"R [1,3) T|T2", "R [7,9) T7|T8"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSyncRegionsEdited(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	// The region taken ours and then edited by hand: its lines are of no
	// side, and it stands where it did.
	got := syncOf(base, ours, theirs, []string{"a", "O", "O2 edited", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	want := []string{"B [1,3) O|O2 edited", "B [7,9) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSyncRegionsGrown(t *testing.T) {
	base := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	ours := []string{"a", "O", "O2", "O3", "d", "e", "f", "g", "H", "I", "j", "k", "l"}
	theirs := []string{"a", "T", "T2", "d", "e", "f", "g", "T7", "T8", "j", "k", "l"}
	// The first region taken ours, three lines where two were: the second
	// region is pushed down one.
	got := syncOf(base, ours, theirs, []string{"a", "O", "O2", "O3", "d", "e", "f", "g", "h", "i", "j", "k", "l"})
	want := []string{"L [1,4) O|O2|O3", "B [8,10) h|i"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// mergeRepo makes the harness's folder a work tree where merging side
// into main stops for a conflict of shared.txt, which both branches
// changed at the same lines. The merge itself may fail: that is the
// conflict.
func (hs *harness) mergeRepo(base, ours, theirs string) func(args ...string) string {
	t := hs.t
	git := hs.gitRepo()
	os.WriteFile(hs.path("shared.txt"), []byte(base), 0o644)
	git("add", "shared.txt")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "side")
	os.WriteFile(hs.path("shared.txt"), []byte(theirs), 0o644)
	git("add", "shared.txt")
	git("commit", "-qm", "theirs")
	git("checkout", "-q", "main")
	os.WriteFile(hs.path("shared.txt"), []byte(ours), 0o644)
	git("add", "shared.txt")
	git("commit", "-qm", "ours")
	cmd := exec.Command("git", "-C", hs.root, "merge", "side")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge went clean: %s", out)
	}
	hs.settleGit()
	return git
}

// mergeOf opens the merge view of the harness's conflict and returns it,
// settled.
func (hs *harness) mergeOf() *mergeView {
	t := hs.t
	path := hs.path("shared.txt")
	if e := hs.a.git.entries[path]; !e.Conflicted() {
		t.Fatalf("shared.txt is not in conflict: %+v", e)
	}
	hs.a.openMerge(path, true)
	hs.settle()
	tm := hs.a.activeTab()
	if tm == nil || tm.merge == nil {
		t.Fatalf("no merge tab: %+v", tm)
	}
	return tm.merge
}

func TestMergeViewOpens(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	if len(v.regions) != 1 {
		t.Fatalf("got %d regions, want 1", len(v.regions))
	}
	// The result starts as the base, not as the file's conflict markers.
	if v.doc == nil || v.doc.buf.String() != "a\nm\nz\n" {
		t.Fatalf("the result is not the base: %q", v.doc.buf.String())
	}
	// The result starts as the base: taking nothing, the conflicts are
	// all unresolved.
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("the result is not the base: %q", got)
	}
	// The sides are named as the merge makes them, and the result says it
	// starts as the base.
	for _, s := range []string{"OURS · main", "THEIRS · side", "RESULT · FROM BASE", "1 of 1"} {
		if !hs.tt.HasText(s) {
			t.Errorf("no %q: %q", s, hs.tt.Texts())
		}
	}
	if got := v.stateOf(v.regions[0]); got != regionUndecided {
		t.Errorf("state %v, want undecided", got)
	}
	// The command stopped for the conflict does not go on while it stands.
	a := hs.a
	a.continueOp()
	hs.settle()
	if st := a.git.status; st == nil || st.Op != gitpkg.OpMerge {
		t.Errorf("the merge went on with the conflict unresolved: %+v", st)
	}
	if a.fails == 0 {
		t.Error("the app does not say why the merge did not go on")
	}
	// One of the conflicts taken, for the picture of the view at work.
	hs.a.takeMergeSide(v, 0, false)
	hs.settle()
	hs.shot("merge-view")
}

func TestMergeViewAcceptAndUndo(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	// Accepting ours puts its lines in the result, as one step of undo.
	a.takeMergeSide(v, 0, true)
	if got := v.doc.buf.String(); got != "a\nours\nz\n" {
		t.Fatalf("after accepting ours: %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionLeft {
		t.Errorf("state %v, want ours", st)
	}
	// Undo brings the base's lines back, and the state with them; redo
	// puts ours in again.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	a.invalidate()
	hs.settle()
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Fatalf("after undo: %q", got)
	}
	if st := v.stateOf(v.regions[0]); st != regionUndecided {
		t.Errorf("after undo the state is %v, want undecided", st)
	}
	if !v.ed.redo() {
		t.Fatal("no step of redo")
	}
	a.invalidate()
	hs.settle()
	if st := v.stateOf(v.regions[0]); st != regionLeft {
		t.Errorf("after redo the state is %v, want ours", st)
	}
	// Ignoring ours takes it back out; accepting theirs takes its lines.
	a.takeMergeSide(v, 0, false)
	if st := v.stateOf(v.regions[0]); st != regionRight {
		t.Errorf("state %v, want theirs", st)
	}
	a.dropMergeSide(v, 0, false)
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("after ignoring theirs: %q", got)
	}
}

func TestMergeViewResolveUsing(t *testing.T) {
	hs := newHarness(t, true)
	// Two conflicts: both sides change the first and the last lines, with
	// a line neither changed between.
	hs.mergeRepo("top\nm\nz\nend\n", "OURS\nm\nz\nOURS2\n", "SIDE\nm\nz\nSIDE2\n")
	v := hs.mergeOf()
	if len(v.regions) != 2 {
		t.Fatalf("got %d regions, want 2", len(v.regions))
	}
	hs.a.resolveUsingMerge(v, true)
	if got := v.doc.buf.String(); got != "OURS\nm\nz\nOURS2\n" {
		t.Errorf("resolve using ours: %q", got)
	}
	if v.unresolved() != 0 {
		t.Errorf("%d unresolved, want 0", v.unresolved())
	}
	// The undo of the whole resolution is one step, back to the base.
	if !v.ed.undo() {
		t.Fatal("no step of undo")
	}
	hs.a.invalidate()
	hs.settle()
	if got := v.resultLines(); !slices.Equal(got, []string{"top", "m", "z", "end"}) {
		t.Errorf("after undo: %q", got)
	}
	if v.unresolved() != 2 {
		t.Errorf("%d unresolved, want 2", v.unresolved())
	}
}

func TestMergeViewComplete(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	path := hs.path("shared.txt")
	// Resolving with theirs and staging the file ends its conflict.
	a.takeMergeSide(v, 0, false)
	a.completeMerge(v)
	hs.settleGit()
	if e := a.git.entries[path]; e.Conflicted() || e.Letter() != 'M' {
		t.Errorf("after marking resolved: %+v", e)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "a\nside\nz\n" {
		t.Errorf("the file has %q (%v)", b, err)
	}
	if !a.mergeResolved(v) {
		t.Error("the view does not say the file stands resolved")
	}
	if !hs.tt.HasText("Mark Resolved") {
		t.Errorf("no actions of a resolved file: %q", hs.tt.Texts())
	}
	hs.shot("merge-resolved")
	// The merge goes on, its conflicts gone.
	a.continueOp()
	hs.settleGit()
	if st := a.git.status; st == nil || st.Op != "" {
		t.Errorf("the merge does not end: %+v", st)
	}
	if b, err := exec.Command("git", "-C", hs.root, "log", "--oneline", "-1").CombinedOutput(); err == nil {
		t.Log("after continue:", strings.TrimSpace(string(b)))
	}
	_ = git
}

func TestMergeViewExitAndContinue(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	// A result taken as ours and saved: the file has it, the conflict
	// still open.
	a.takeMergeSide(v, 0, true)
	a.saveMerge(v)
	hs.settleGit()
	if b, err := os.ReadFile(hs.path("shared.txt")); err != nil || string(b) != "a\nours\nz\n" {
		t.Fatalf("the result is not saved: %q (%v)", b, err)
	}
	if e := a.git.entries[hs.path("shared.txt")]; !e.Conflicted() {
		t.Fatalf("the conflict is not open: %+v", e)
	}
	// Out of the merge, and back in: the result goes on where it stood.
	a.closeTab(a.activeTab(), nil)
	hs.settle()
	a.openMerge(hs.path("shared.txt"), true)
	hs.settle()
	v2 := a.activeTab().merge
	if v2 == nil || v2.doc == nil {
		t.Fatal("no merge tab back")
	}
	if got := v2.doc.buf.String(); got != "a\nours\nz\n" {
		t.Errorf("the result did not go on: %q", got)
	}
	if st := v2.stateOf(v2.regions[0]); st != regionLeft {
		t.Errorf("the state did not go on: %v", st)
	}
}

func TestMergeViewRestoreConflict(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	a := hs.a
	path := hs.path("shared.txt")
	a.takeMergeSide(v, 0, false)
	a.completeMerge(v)
	hs.settleGit()
	if e := a.git.entries[path]; e.Conflicted() {
		t.Fatalf("the file did not stand resolved: %+v", e)
	}
	// Putting it back in conflict brings the stages and the markers, and
	// the view starts the result over at the base.
	a.restoreMerge(v)
	if a.confirm == nil {
		t.Fatal("no confirm")
	}
	a.confirm.run()
	a.confirm = nil
	hs.settleGit()
	if e := a.git.entries[path]; !e.Conflicted() {
		t.Fatalf("the file is not in conflict again: %+v", e)
	}
	if b, err := os.ReadFile(path); err != nil || !bytes.Contains(b, []byte("<<<<<<<")) {
		t.Errorf("the markers are not back: %q (%v)", b, err)
	}
	if got := v.resultLines(); !slices.Equal(got, []string{"a", "m", "z"}) {
		t.Errorf("the result is not the base: %q", got)
	}
	if v.unresolved() != 1 {
		t.Errorf("%d unresolved, want 1", v.unresolved())
	}
}

func TestMergeViewTypeResult(t *testing.T) {
	hs := newHarness(t, true)
	hs.mergeRepo("a\nm\nz\n", "a\nours\nz\n", "a\nside\nz\n")
	v := hs.mergeOf()
	// Typing in the result pane edits the result: the conflict's lines
	// become lines of its own, and the regions follow the text.
	v.panes[1].scroll.Focus()
	hs.settle()
	hs.tt.Type("edit ")
	hs.settle()
	text := v.doc.buf.String()
	if !strings.Contains(text, "edit ") {
		t.Errorf("the typing did not land in the result: %q", text)
	}
	if v.gen != v.doc.buf.Version() {
		t.Error("the regions did not follow the text")
	}
}

package main

import (
	"os"
	"testing"
)

func TestDiffWhitespaceModes(t *testing.T) {
	lines := []string{"a  ", "  ", "x y"}
	if got := compareDiffLines(lines, whitespaceExact); got[0] != "a  " {
		t.Fatalf("exact changed: %#v", got)
	}
	if got := compareDiffLines(lines, whitespaceEOL); got[0] != "a" {
		t.Fatalf("eol: %#v", got)
	}
	if got := compareDiffLines(lines, whitespaceAll); got[2] != "xy" {
		t.Fatalf("all: %#v", got)
	}
	if got := compareDiffLines(lines, whitespaceBlank); got[1] != "" {
		t.Fatalf("blank: %#v", got)
	}
}

func TestDiffIgnoreLineEndHidesOnlyWhitespaceChange(t *testing.T) {
	hs := newHarness(t, true)
	git := hs.gitRepo()
	p := hs.path("spaces.txt")
	os.WriteFile(p, []byte("one\ntwo\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	os.WriteFile(p, []byte("one  \ntwo\n"), 0o644)
	hs.a.settings.DiffWhitespace = "eol"
	hs.settleGit()
	hs.a.showChanges(diffTarget{path: p, base: diffIndex}, true)
	hs.settleDiff(hs.a.activeTab().diff)
	if got := len(hs.a.activeTab().diff.files[0].hunks); got != 0 {
		t.Fatalf("line-end-only change has %d hunks", got)
	}
}

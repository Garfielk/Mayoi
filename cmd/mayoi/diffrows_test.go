package main

import (
	"slices"
	"testing"

	"mayoi/internal/linediff"
)

// TestLineRowsPairsAlike: within a run of changes, a line pairs with the
// new line it is alike enough to show as changed from; lines no pair
// takes stand as a removal and an addition of their own, as the two
// comments here do — alike in hardly a word, though they stand where the
// positional pairing would tie them together.
func TestLineRowsPairsAlike(t *testing.T) {
	old := []string{
		"// Note: originId 关联与会话话费刷新 — 见 .agents/notes/implemented/feature/2026-10-01",
		"export function poller({ initialOriginIds, query, onProgress }: IOptions) {",
		"\tconst pending = new Set(initialOriginIds.filter(Boolean));",
		"\tconst completed = new Set<string>();",
	}
	new := []string{
		"// Note: 历史功能化、首次延迟与旧值偏离 — 见 .agents/notes/bug-fix/2026-10-16",
		"export function poller(query: string, onProgress: Fn): IOptions {",
		"\tconst pending = new Set<string>();",
		"\tconst done = new Set<string>();",
	}
	hunks := linediff.Hunks(old, new)
	if len(hunks) != 1 {
		t.Fatalf("hunks %d, want 1", len(hunks))
	}
	h := hunks[0]
	if h.ALen != 4 || h.BLen != 4 {
		t.Fatalf("hunk %dx%d, want 4x4", h.ALen, h.BLen)
	}
	_, pairs := lineRows(old, new, hunks)
	var inHunk []diffRow
	for _, r := range pairs {
		if r.hunk == 0 && r.kind == rowLine {
			inHunk = append(inHunk, r)
		}
	}
	// The comment pairs with nothing: it stands as a removal, and the new
	// comment as an addition of its own at the end.
	if inHunk[0].old != 0 || inHunk[0].new != -1 {
		t.Errorf("the old comment is paired: %+v", inHunk[0])
	}
	last := inHunk[len(inHunk)-1]
	if last.old != -1 || last.new != 0 {
		t.Errorf("the new comment is paired: %+v", last)
	}
	// The three lines of code are alike enough to pair, in order.
	for n, want := range []int{1, 2, 3} {
		r := inHunk[n+1]
		if r.old != want || r.new != want {
			t.Errorf("code line %d: row %+v, want %d paired with %d", n, r, want, want)
		}
	}
	// The unified layout takes the same pairing: the removed comment has
	// no peer, so no word of it shows as changed from the new comment.
	unified, _ := lineRows(old, new, hunks)
	removed := slices.DeleteFunc(slices.Clone(unified), func(r diffRow) bool {
		return !(r.kind == rowLine && r.hunk == 0 && r.old == 0)
	})
	if len(removed) != 1 || removed[0].peer != -1 {
		t.Errorf("the removed comment's peer: %+v", removed)
	}
}

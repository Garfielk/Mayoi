package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"mayoi/internal/linediff"
)

// rowKind is what a row of a diff shows.
type rowKind uint8

const (
	rowLine    rowKind = iota // a line of each side, or of one
	rowGap                    // unchanged lines folded away
	rowHunk                   // the header of a run of changes and the lines around them
	rowSection                // the header of the changes staged, or those not
	rowFile                   // the header of a file
	rowNote                   // why the lines of a file do not show
	rowSpace                  // the room between two files
	rowCommit                 // the message of the commit a diff shows
	rowComment                // the card of a note, n of the review's
)

// diffRow is a row of a diff as it shows, of file, one of the diff's.
//
// A line row has a line of each side, -1 where a side has none, and the
// hunk it is part of, -1 for lines unchanged. In the unified layout it
// has one side when it changed, and peer is the line of the other side
// it was modified from, or into, -1 for none.
//
// gap is the key of the gap a gap row folds, or that a line row shows as
// it opened, -1 for none, and n the lines the gap folds, or for the card
// of a note, the note's number, as review.noteAt takes it. label is what a
// header or a note says, and h how many lines tall the row is, as its
// line wraps.
type diffRow struct {
	kind                      rowKind
	file                      int
	old, new, hunk, peer, gap int
	n, h                      int
	label                     string
}

// lineRow is the row of a line of each side.
func lineRow(old, new, hunk, peer int) diffRow {
	return diffRow{kind: rowLine, old: old, new: new, hunk: hunk, peer: peer, gap: -1, h: 1}
}

// between reports whether a row is one the keys pass over: the room
// between files, or a header of more than one.
func (r *diffRow) between() bool {
	return r.kind == rowSpace || r.kind == rowSection || r.kind == rowCommit
}

// changed reports whether a row is a line changed.
func (r *diffRow) changed() bool { return r.kind == rowLine && r.hunk >= 0 }

// lineRows lays out every line of two sides and the hunks between them:
// in one column, the lines of each hunk removed before those added, and
// side by side, the lines of a hunk across from each other.
func lineRows(old, new []string, hunks []linediff.Hunk) (unified, pairs []diffRow) {
	unified = make([]diffRow, 0, max(len(old), len(new)))
	pairs = make([]diffRow, 0, max(len(old), len(new)))
	i, j := 0, 0
	same := func(to int) {
		for ; i < to; i, j = i+1, j+1 {
			r := lineRow(i, j, -1, -1)
			unified, pairs = append(unified, r), append(pairs, r)
		}
	}
	for k, h := range hunks {
		same(h.A)
		ofOld, ofNew := hunkPeers(old, new, h)
		for n := range h.ALen {
			unified = append(unified, lineRow(h.A+n, -1, k, ofOld[n]))
		}
		for m := range h.BLen {
			unified = append(unified, lineRow(-1, h.B+m, k, ofNew[m]))
		}
		// The pairs stand in the old lines' order; a line no pair took
		// stands as a removal of its own, and the new lines none took
		// follow as additions of their own.
		for n := range h.ALen {
			if ofOld[n] < 0 {
				pairs = append(pairs, lineRow(h.A+n, -1, k, -1))
				continue
			}
			pairs = append(pairs, lineRow(h.A+n, ofOld[n], k, h.A+n))
		}
		for m := range h.BLen {
			if ofNew[m] < 0 {
				pairs = append(pairs, lineRow(-1, h.B+m, k, -1))
			}
		}
		i, j = h.A+h.ALen, h.B+h.BLen
	}
	same(len(old))
	return unified, pairs
}

// alikeEnough is how much two lines must hold the same for the change of
// one into the other to show as a modification of a pair; less alike,
// and they stand as a removal and an addition, which two lines that are
// hardly alike read better as.
const alikeEnough = 0.35

// alikeMax is the most lines of a change times lines that pairing by
// how alike the lines are takes: bigger changes pair by their places,
// as they did, rather than compare every line with every line.
const alikeMax = 64 * 64

// hunkPeers pairs the changed lines of a hunk: which new line each old
// line shows as changed from, and which old line each new line came
// from, -1 for none. The pairs that hold the most alike take each other
// first, one line to one line; lines no pair takes stand alone. Changes
// of more lines than alikeMax holds pair by their places, as the lines
// stand in the change.
func hunkPeers(old, new []string, h linediff.Hunk) (ofOld, ofNew []int) {
	ofOld, ofNew = make([]int, h.ALen), make([]int, h.BLen)
	for i := range ofOld {
		ofOld[i] = -1
	}
	for i := range ofNew {
		ofNew[i] = -1
	}
	m := min(h.ALen, h.BLen)
	if h.ALen == 0 || h.BLen == 0 {
		return ofOld, ofNew
	}
	if h.ALen*h.BLen > alikeMax {
		for i := range m {
			ofOld[i], ofNew[i] = i, i
		}
		return ofOld, ofNew
	}
	// The pairs that hold the most alike take each other first; then a
	// line still standing alone takes a new line from another pair when
	// that line's own pair has another new line alike enough to it —
	// with the pairs of a tie taken in the lines' order, so that the
	// matching keeps to the change as it reads.
	type pair struct {
		score float64
		i, j  int
	}
	score := make([][]float64, h.ALen)
	ps := make([]pair, 0, h.ALen*h.BLen)
	for i := range h.ALen {
		score[i] = make([]float64, h.BLen)
		for j := range h.BLen {
			s := alikeRatio(old[h.A+i], new[h.B+j])
			score[i][j] = s
			if s >= alikeEnough {
				ps = append(ps, pair{s, i, j})
			}
		}
	}
	slices.SortStableFunc(ps, func(a, b pair) int { return cmp.Compare(b.score, a.score) })
	for _, p := range ps {
		if ofOld[p.i] < 0 && ofNew[p.j] < 0 {
			ofOld[p.i], ofNew[p.j] = p.j, p.i
		}
	}
	for changed := true; changed; {
		changed = false
		for i := range ofOld {
			if ofOld[i] >= 0 {
				continue
			}
			taken := false
			for j := range ofNew {
				if ofNew[j] < 0 && score[i][j] >= alikeEnough {
					ofOld[i], ofNew[j] = j, i
					changed, taken = true, true
					break
				}
			}
			if taken {
				continue
			}
			// Every new line alike to i is taken: one of them moves
			// aside for i when its own pair has another new line free
			// that is alike enough to it.
			for j := range ofNew {
				if ofNew[j] < 0 || score[i][j] < alikeEnough {
					continue
				}
				holder := ofNew[j]
				for j2 := range ofNew {
					if ofNew[j2] >= 0 || score[holder][j2] < alikeEnough {
						continue
					}
					ofOld[i], ofNew[j] = j, i
					ofOld[holder], ofNew[j2] = j2, holder
					changed = true
					break
				}
				if changed {
					break
				}
			}
		}
	}
	// The matching stood in the lines of the change: out to where they
	// are in the sides.
	for i := range ofOld {
		if ofOld[i] >= 0 {
			ofOld[i] += h.B
		}
	}
	for j := range ofNew {
		if ofNew[j] >= 0 {
			ofNew[j] += h.A
		}
	}
	return ofOld, ofNew
}

// alikeRatio scores how much of two lines stays when one is changed
// into the other, as the share of the runes the word diff keeps: 1 for
// the same text, near 0 for a rewrite. A pair shows as a modification
// while what changed is the lesser of the line, which is what the
// highlight shows of a pair.
func alikeRatio(a, b string) float64 {
	if a == b {
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	if min(len(ra), len(rb))*5 < max(len(ra), len(rb))*2 {
		return 0
	}
	as, bs := linediff.Words(a, b)
	changed := func(spans []linediff.Span, total int) int {
		n := 0
		for _, s := range spans {
			n += s.End - s.Start
		}
		return min(n, total)
	}
	worst := max(changed(as, len(ra)), changed(bs, len(rb)))
	return 1 - float64(worst)/float64(max(len(ra), len(rb)))
}

// span is a run of rows, from up to to.
type span struct{ from, to int }

// foldRows adds the rows of file f, number file of the diff, to out, from
// all its line rows of the layout shown: the unchanged lines more than
// context away from a change folded into a gap, unless the gap is open,
// and a header before each run of changes and the lines around them.
// With full set, every line shows, and no header.
func foldRows(out []diffRow, f *diffFile, file int, all []diffRow, full bool, context int) []diffRow {
	if full {
		for _, r := range all {
			r.file = file
			out = append(out, r)
		}
		return out
	}
	// How many lines of each side come before each row.
	oldAt, newAt := make([]int, len(all)+1), make([]int, len(all)+1)
	for i, r := range all {
		oldAt[i+1], newAt[i+1] = oldAt[i], newAt[i]
		if r.old >= 0 {
			oldAt[i+1]++
		}
		if r.new >= 0 {
			newAt[i+1]++
		}
	}
	// The runs of unchanged lines that fold, and those open.
	var folds, opens []span
	for i := 0; i < len(all); {
		if all[i].hunk >= 0 {
			i++
			continue
		}
		j := i
		for j < len(all) && all[j].hunk < 0 {
			j++
		}
		from, to := i, j
		if i > 0 {
			from += context
		}
		if j < len(all) {
			to -= context
		}
		if to-from >= 2 {
			if f.open[newAt[from]] {
				opens = append(opens, span{from, to})
			} else {
				folds = append(folds, span{from, to})
			}
		}
		i = j
	}
	section := func(from, to int) {
		if from >= to {
			return
		}
		changes := false
		for _, r := range all[from:to] {
			changes = changes || r.hunk >= 0
		}
		if changes {
			out = append(out, diffRow{kind: rowHunk, file: file, old: -1, new: -1, hunk: -1, peer: -1, gap: -1, h: 1,
				label: hunkHeader(f, oldAt[from], oldAt[to]-oldAt[from], newAt[from], newAt[to]-newAt[from])})
		}
		for i := from; i < to; i++ {
			r := all[i]
			r.file = file
			for _, o := range opens {
				if i >= o.from && i < o.to {
					r.gap = newAt[o.from]
				}
			}
			out = append(out, r)
		}
	}
	at := 0
	for _, g := range folds {
		section(at, g.from)
		out = append(out, diffRow{kind: rowGap, file: file, old: all[g.from].old, new: all[g.from].new, hunk: -1, peer: -1,
			gap: newAt[g.from], n: g.to - g.from, h: 1})
		at = g.to
	}
	section(at, len(all))
	return out
}

// hunkHeader is the header of a run of lines of each side, from line
// oldAt of the old side, n of them, and newAt of the new, m of them: as
// Git writes it, with the line opening the block the run starts in.
func hunkHeader(f *diffFile, oldAt, n, newAt, m int) string {
	num := func(at, n int) string {
		if n > 0 {
			at++
		}
		if n == 1 {
			return fmt.Sprint(at)
		}
		return fmt.Sprintf("%d,%d", at, n)
	}
	h := fmt.Sprintf("@@ -%s +%s @@", num(oldAt, n), num(newAt, m))
	s, line := &f.new, newAt
	if !f.new.ok || len(f.new.lines) == 0 {
		s, line = &f.old, oldAt
	}
	if line <= 0 || line >= len(s.lines) {
		return h
	}
	if scopes := scopesIn(len(s.lines), line, s.window()); len(scopes) > 0 {
		t := strings.TrimSpace(s.lines[scopes[len(scopes)-1]])
		if utf8.RuneCountInString(t) > 80 {
			t = string([]rune(t)[:79]) + "…"
		}
		h += " " + t
	}
	return h
}

// window returns the lines of a side as a window over all of them.
func (s *diffSide) window() *lineWindow {
	if s.win == nil {
		w := &lineWindow{}
		if s.src != nil {
			w.text = s.src.text
			w.starts = append(append(make([]int, 0, len(s.src.starts)+1), s.src.starts...), len(s.src.text)+1)
		}
		s.win = w
	}
	return s.win
}

// wrapAt returns where the rows of a line after the first start, as it
// wraps at cols columns, in bytes of shown, the line with its tabs
// expanded: after the last space that fits, when there is one past the
// middle of the row, or else at the edge.
func wrapAt(shown string, cols int) []int {
	if cols < 1 || len(shown) <= cols {
		return nil // as long in bytes as in columns, or longer
	}
	var out []int
	start, col := 0, 0
	space, spaceCol := -1, 0 // after the row's last space, and its column
	for i, r := range shown {
		if col >= cols {
			if space > start && spaceCol > cols/2 {
				out = append(out, space)
				start, col = space, col-spaceCol
			} else {
				out = append(out, i)
				start, col = i, 0
			}
			space = -1
		}
		col++
		if r == ' ' {
			space, spaceCol = i+1, col
		}
	}
	return out
}

// wrapCount returns how many rows a line takes as it wraps at cols
// columns.
func wrapCount(text string, cols int) int {
	if cols < 1 || len(text) <= cols && strings.IndexByte(text, '\t') < 0 {
		return 1
	}
	shown, _ := expandTabs(clipLine(text))
	return len(wrapAt(shown, cols)) + 1
}

// clipLine returns as much of a line as a diff shows: the rest of a
// longer one, as of a minified file, is cut.
func clipLine(text string) string {
	if len(text) > maxDiffLine {
		return strings.ToValidUTF8(text[:maxDiffLine], "")
	}
	return text
}

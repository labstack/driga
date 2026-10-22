package hooks

import (
	"slices"
	"testing"
)

func TestLineEditsAreShortestAndReconstructBothFiles(t *testing.T) {
	// Exhaust repeated lines: a prefix/suffix replacement or greedy matching
	// produces non-minimal scripts for many of these pairs.
	sequences := [][]string{nil}
	for depth := 0; depth < 4; depth++ {
		previous := slices.Clone(sequences)
		for _, s := range previous {
			if len(s) != depth {
				continue
			}
			for _, line := range []string{"a\n", "b\n"} {
				sequences = append(sequences, append(slices.Clone(s), line))
			}
		}
	}
	for _, before := range sequences {
		for _, after := range sequences {
			var old, next []string
			changes := 0
			for _, e := range lineEdits(before, after) {
				if e.kind != '+' {
					old = append(old, e.text)
				}
				if e.kind != '-' {
					next = append(next, e.text)
				}
				if e.kind != ' ' {
					changes++
				}
			}
			// Independent LCS oracle verifies the minimal number of edits.
			row := make([]int, len(after)+1)
			for _, a := range before {
				diagonal := 0
				for j, b := range after {
					prev := row[j+1]
					if a == b {
						row[j+1] = diagonal + 1
					} else {
						row[j+1] = max(row[j], row[j+1])
					}
					diagonal = prev
				}
			}
			want := len(before) + len(after) - 2*row[len(after)]
			if !slices.Equal(old, before) || !slices.Equal(next, after) || changes != want {
				t.Fatalf("%q → %q: reconstructed %q/%q, edits=%d want=%d", before, after, old, next, changes, want)
			}
		}
	}
}

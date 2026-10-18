package gitscan

import (
	"reflect"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	out := "worktree /repo main/ü\nHEAD abc\nbranch refs/heads/main\n\nworktree /linked\nHEAD def\nbranch refs/heads/feat/x\nlocked reason\n\nworktree /detached\nHEAD 123\ndetached\n\nworktree /bare\nbare\n"
	want := []WorktreeEntry{{Path: "/repo main/ü", Head: "abc", Branch: "main"}, {Path: "/linked", Head: "def", Branch: "feat/x"}, {Path: "/detached", Head: "123", Detached: true}, {Path: "/bare", Bare: true}}
	if got := ParseWorktreeList([]byte(out)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseStatusV2(t *testing.T) {
	out := "# branch.oid abc\n# branch.head feat/x\n# branch.ab +2 -1\n1 .M N... 100644 100644 100644 abc abc file one\n1 D. N... 100644 000000 000000 abc abc gone\n2 R. N... 100644 100644 100644 abc abc R100 new ü\told name\nu UU N... 100644 100644 100644 100644 abc abc abc conflict\n? untracked\n! ignored\nmalformed\n"
	want := Status{Branch: "feat/x", Ahead: 2, Behind: 1, Dirty: 4, DirtyPaths: []string{"file one", "gone", "new ü", "conflict"}}
	if got := ParseStatusV2([]byte(out)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseStatusV2NoUpstream(t *testing.T) {
	for _, branch := range []string{"main", "(detached)"} {
		got := ParseStatusV2([]byte("# branch.head " + branch + "\n"))
		if got.Ahead != 0 || got.Behind != 0 || got.Dirty != 0 || got.Branch != branch {
			t.Fatal(got)
		}
	}
}

func TestParseQuotedPaths(t *testing.T) {
	w := ParseWorktreeList([]byte("worktree \"/repo\\nname\"\nHEAD abc\ndetached\n"))
	s := ParseStatusV2([]byte("1 .M N... 100644 100644 100644 abc abc \"file\\tname\"\n2 R. N... 100644 100644 100644 abc abc R100 \"new\\nname\"\told\n"))
	if len(w) != 1 || w[0].Path != "/repo\nname" || !reflect.DeepEqual(s.DirtyPaths, []string{"file\tname", "new\nname"}) {
		t.Fatalf("worktrees=%#v status=%#v", w, s)
	}
}

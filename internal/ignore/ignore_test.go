package ignore

import (
	"runtime"
	"testing"
)

func TestMatchFileBaseName(t *testing.T) {
	m := New([]string{"*.tmp", "pagefile.sys"})

	yes := []string{"a.tmp", "deep/nested/path/b.tmp", "pagefile.sys", "c:/pagefile.sys"}
	for _, p := range yes {
		if !m.MatchFile(p) {
			t.Errorf("%q should match a base-name pattern", p)
		}
	}

	no := []string{"a.txt", "tmp", "pagefile.sys.bak"}
	for _, p := range no {
		if m.MatchFile(p) {
			t.Errorf("%q should not match", p)
		}
	}
}

func TestMatchFileFullPath(t *testing.T) {
	m := New([]string{"posts/drafts/*.html"})

	if !m.MatchFile("posts/drafts/one.html") {
		t.Error("a path pattern did not match its path")
	}
	if m.MatchFile("posts/one.html") {
		t.Error("a path pattern matched the wrong directory")
	}
	// "*" does not cross a separator.
	if m.MatchFile("posts/drafts/deeper/one.html") {
		t.Error("a single star crossed a path separator")
	}
}

func TestMatchDirPrunes(t *testing.T) {
	m := New([]string{"media/", "node_modules/"})

	if !m.MatchDir("media") {
		t.Error("a directory pattern did not match")
	}
	if !m.MatchDir("export/deep/media") {
		t.Error("a directory pattern should match at any depth")
	}
	if m.MatchFile("media") {
		t.Error("a directory pattern matched a file")
	}
}

func TestLeadingDoubleStarIsStripped(t *testing.T) {
	m := New([]string{"**/media/"})

	if !m.MatchDir("a/b/media") {
		t.Error("**/ prefix broke matching at depth")
	}
}

func TestCommentsAndBlanksAreSkipped(t *testing.T) {
	m := New([]string{"", "   ", "# a comment", "*.tmp"})

	if got := m.Patterns(); len(got) != 1 {
		t.Errorf("compiled %d patterns, expected 1: %v", len(got), got)
	}
	if m.MatchFile("a comment") {
		t.Error("a comment was compiled into a pattern")
	}
}

func TestNilMatcherMatchesNothing(t *testing.T) {
	var m *Matcher

	if !m.Empty() {
		t.Error("a nil matcher should report empty")
	}
	if m.MatchFile("anything") || m.MatchDir("anything") {
		t.Error("a nil matcher matched something")
	}
}

// TestPatternsAreSorted guards the reason the sort exists: two of the four rule
// categories are maps, and Go randomises map iteration. Unsorted, a verbose run
// would list its rules in a different order every time.
func TestPatternsAreSorted(t *testing.T) {
	m := New([]string{"zebra", "alpha", "middle", "*.glob", "dir/"})

	first := m.Patterns()
	for i := 0; i < 20; i++ {
		got := m.Patterns()
		if len(got) != len(first) {
			t.Fatalf("Patterns() returned %d entries, then %d", len(first), len(got))
		}
		for j := range first {
			if got[j] != first[j] {
				t.Fatalf("Patterns() is not stable: %v then %v", first, got)
			}
		}
	}

	// Directory rules come first, with their trailing slash restored.
	if first[0] != "dir/" {
		t.Errorf("directory rules should sort first: %v", first)
	}
}

// TestCaseSensitivityFollowsTheBuildTarget checks the deliberate GOOS decision:
// the platform being compiled *for* decides, so a cross-compiled Linux binary
// stays case-sensitive.
func TestCaseSensitivityFollowsTheBuildTarget(t *testing.T) {
	m := New([]string{"PageFile.sys"})

	got := m.MatchFile("pagefile.sys")
	want := runtime.GOOS == "windows"

	if got != want {
		t.Errorf("matching %q against %q gave %v; on %s it should be %v",
			"pagefile.sys", "PageFile.sys", got, runtime.GOOS, want)
	}
}

// TestBackslashesFollowTheBuildTarget pins behaviour that is correctly different
// per platform, and that it is tempting to get wrong in exactly one direction.
//
// filepath.ToSlash rewrites separators on Windows and is a no-op on Linux, where
// a backslash is an ordinary, legal character in a filename rather than a
// separator. So `posts\drafts` is a path on Windows and a single oddly-named
// directory on Linux, and the matcher is right to treat them differently.
//
// Asserting the Windows answer unconditionally is the mistake this test used to
// make. It passed locally and failed in CI, which is the whole reason the race
// detector and the Linux build live there.
func TestBackslashesFollowTheBuildTarget(t *testing.T) {
	m := New([]string{"posts/drafts/"})

	got := m.MatchDir(`posts\drafts`)
	want := runtime.GOOS == "windows"

	if got != want {
		if want {
			t.Errorf("a Windows-style path did not match a forward-slash pattern")
			return
		}
		t.Errorf("a backslash was treated as a separator on %s, where it is a legal filename character", runtime.GOOS)
	}

	// Forward slashes are the portable form and must match everywhere.
	if !m.MatchDir("posts/drafts") {
		t.Error("a forward-slash path did not match its own pattern")
	}
}

func TestNoiseListCompiles(t *testing.T) {
	m := New(Noise)

	if m.Empty() {
		t.Fatal("the built-in noise list compiled to nothing")
	}
	if !m.MatchFile("a/b/photo.JPG") && runtime.GOOS == "windows" {
		t.Error("case-insensitive matching is not working on Windows")
	}
	if !m.MatchDir("some/path/.git") {
		t.Error(".git should be pruned")
	}
}

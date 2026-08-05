// Package ignore implements the pattern matching behind coroner's exclude
// rules. It is carried over from hecato unchanged, because the job is the same
// one: keep a walk out of trees it has no business in, cheaply enough that the
// matching does not become the bottleneck.
//
// Pattern rules, in full:
//
//   - Paths are normalised to forward slashes before matching, and lowercased
//     on Windows so that patterns are case-insensitive there and case-sensitive
//     everywhere else. That matches how the underlying filesystems behave.
//
//   - A pattern ending in "/" is a directory pattern. When a directory matches,
//     the walk does not descend into it at all. This is the difference between
//     ignoring a large tree and merely hiding it: pruning costs nothing, while
//     filtering still pays to walk every file underneath. It matters here
//     because a Facebook export is mostly directories coroner has no use for.
//
//   - A pattern containing no "/" matches against the base name only, at any
//     depth. "*.tmp" matches a/b/c.tmp.
//
//   - A pattern containing "/" matches against the whole path.
//
//   - A leading "**/" is stripped, because matching at any depth is already the
//     default for base-name patterns. It is accepted so that "**/media" reads
//     the way people expect.
//
// Wildcards are those of path/filepath.Match: "*", "?" and "[...]". Note that
// "*" does not cross a path separator.
package ignore

import (
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ruleSet holds one category of patterns, split by how expensive it is to test.
// Most patterns in practice are literal names with no wildcard in them, and
// those can be a map lookup rather than a path.Match call.
type ruleSet struct {
	literalBase map[string]struct{}
	literalPath map[string]struct{}

	globBase []string
	globPath []string
}

func (r *ruleSet) empty() bool {
	return len(r.literalBase) == 0 && len(r.literalPath) == 0 &&
		len(r.globBase) == 0 && len(r.globPath) == 0
}

// needsFullPath reports whether any rule here looks at more than the base name.
// When none do, callers skip normalising the whole path.
func (r *ruleSet) needsFullPath() bool {
	return len(r.literalPath) > 0 || len(r.globPath) > 0
}

func (r *ruleSet) add(p string) {
	hasGlob := strings.ContainsAny(p, "*?[")
	hasSlash := strings.Contains(p, "/")

	switch {
	case !hasGlob && !hasSlash:
		if r.literalBase == nil {
			r.literalBase = make(map[string]struct{})
		}
		r.literalBase[p] = struct{}{}
	case !hasGlob:
		if r.literalPath == nil {
			r.literalPath = make(map[string]struct{})
		}
		r.literalPath[p] = struct{}{}
	case hasSlash:
		r.globPath = append(r.globPath, p)
	default:
		r.globBase = append(r.globBase, p)
	}
}

// all returns every pattern in this set, sorted so that reporting is stable
// despite two of the four categories being maps. Go randomises map iteration,
// so without this sort a verbose run would print its rules in a different order
// every time -- harmless in hecato, but here it is the same class of mistake
// that would make a digest non-reproducible.
func (r *ruleSet) all() []string {
	out := make([]string, 0, len(r.literalBase)+len(r.literalPath)+len(r.globBase)+len(r.globPath))
	for p := range r.literalBase {
		out = append(out, p)
	}
	for p := range r.literalPath {
		out = append(out, p)
	}
	out = append(out, r.globBase...)
	out = append(out, r.globPath...)

	sort.Strings(out)
	return out
}

// match tests one path against this rule set, cheapest check first.
func (r *ruleSet) match(target string) bool {
	if r.empty() {
		return false
	}

	base := normalise(filepath.Base(target))

	if _, ok := r.literalBase[base]; ok {
		return true
	}
	for _, p := range r.globBase {
		if ok, err := path.Match(p, base); err == nil && ok {
			return true
		}
	}

	if !r.needsFullPath() {
		return false
	}

	full := normalise(target)

	if _, ok := r.literalPath[full]; ok {
		return true
	}
	for _, p := range r.globPath {
		if ok, err := path.Match(p, full); err == nil && ok {
			return true
		}
	}

	return false
}

// Matcher holds the compiled rules for a single run. A nil *Matcher matches
// nothing, so callers that have no rules can pass nil rather than branching.
//
// A Matcher is read-only once built, so a walk's workers share one safely.
type Matcher struct {
	files ruleSet
	dirs  ruleSet
}

// New compiles patterns into a Matcher. Blank entries and "#" comments are
// skipped so that a pattern list can be read straight out of a config file.
func New(patterns []string) *Matcher {
	m := &Matcher{}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}

		p = normalise(p)
		p = strings.TrimPrefix(p, "**/")
		if p == "" {
			continue
		}

		if strings.HasSuffix(p, "/") {
			m.dirs.add(strings.TrimSuffix(p, "/"))
			continue
		}
		m.files.add(p)
	}
	return m
}

// Empty reports whether the Matcher has no rules.
func (m *Matcher) Empty() bool {
	return m == nil || (m.files.empty() && m.dirs.empty())
}

// MatchDir reports whether a directory should be pruned from the walk.
func (m *Matcher) MatchDir(dirPath string) bool {
	if m == nil {
		return false
	}
	return m.dirs.match(dirPath)
}

// MatchFile reports whether a file should be left out.
func (m *Matcher) MatchFile(filePath string) bool {
	if m == nil {
		return false
	}
	return m.files.match(filePath)
}

// Patterns returns the compiled patterns, directory rules first with their
// trailing slash restored.
func (m *Matcher) Patterns() []string {
	if m == nil {
		return nil
	}

	dirs := m.dirs.all()
	files := m.files.all()

	out := make([]string, 0, len(dirs)+len(files))
	for _, d := range dirs {
		out = append(out, d+"/")
	}
	return append(out, files...)
}

// normalise makes a path or pattern comparable: forward slashes throughout, and
// lowercased on Windows where the filesystem is case-insensitive. Note that
// runtime.GOOS is the *target* of the build, so a cross-compiled linux binary
// correctly stays case-sensitive.
func normalise(s string) string {
	s = filepath.ToSlash(s)
	if runtime.GOOS == "windows" {
		s = strings.ToLower(s)
	}
	return s
}

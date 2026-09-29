// Package detect scores workspace profiles, existing harnesses, and
// capability recommendations from files on disk.
package detect

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/model"
	"github.com/c0dn/ocws/internal/registry"
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true, "vendor": true, "target": true,
	"dist": true, "build": true, "__pycache__": true, ".next": true, ".cache": true, ".ocws": true,
}

const (
	maxDepth   = 6
	maxEntries = 20000
)

// Index is a bounded listing of workspace-relative paths. Directories carry a
// trailing slash variant so patterns like `challenges/` match.
type Index struct {
	Root      string
	Paths     []string
	Truncated bool
}

func Scan(root string) *Index {
	idx := &Index{Root: root}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.Count(rel, "/") >= maxDepth {
				return filepath.SkipDir
			}
			idx.Paths = append(idx.Paths, rel+"/")
		} else {
			idx.Paths = append(idx.Paths, rel)
		}
		if len(idx.Paths) >= maxEntries {
			idx.Truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	return idx
}

// Match returns the first path matching a glob. Patterns ending in "/" only
// match directories; other patterns match files or directories.
func (x *Index) Match(pattern string) (string, bool) {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	dirOnly := strings.HasSuffix(pattern, "/")
	for _, p := range x.Paths {
		isDir := strings.HasSuffix(p, "/")
		if dirOnly && !isDir {
			continue
		}
		candidate := p
		if isDir && !dirOnly {
			candidate = strings.TrimSuffix(p, "/")
		}
		if ok, _ := doublestar.Match(pattern, candidate); ok {
			return candidate, true
		}
	}
	return "", false
}

type ProfileScore struct {
	ProfileID string   `json:"profileId"`
	Name      string   `json:"name"`
	Score     int      `json:"score"`
	Priority  int      `json:"priority"`
	Matches   []string `json:"matches"`
	Fallback  bool     `json:"fallback"`
}

type ProfileDetection struct {
	Best   string         `json:"best"`
	Reason string         `json:"reason"`
	Scores []ProfileScore `json:"scores"`
}

// Profiles ranks registry profiles by `detect.paths` matches.
func Profiles(idx *Index, reg *registry.Registry) ProfileDetection {
	var out ProfileDetection
	var fallback string
	for _, id := range reg.Order {
		p := reg.Profiles[id]
		s := ProfileScore{ProfileID: id, Name: p.Name(), Matches: []string{}}
		if p.Detect != nil {
			s.Priority, s.Fallback = p.Detect.Priority, p.Detect.Fallback
			for _, pat := range p.Detect.Paths {
				if hit, ok := idx.Match(pat); ok {
					s.Score++
					s.Matches = append(s.Matches, hit)
				}
			}
			if p.Detect.Fallback && fallback == "" {
				fallback = id
			}
		}
		out.Scores = append(out.Scores, s)
	}
	sort.SliceStable(out.Scores, func(i, j int) bool {
		a, b := out.Scores[i], out.Scores[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Priority > b.Priority
	})
	if len(out.Scores) > 0 && out.Scores[0].Score > 0 {
		out.Best = out.Scores[0].ProfileID
		out.Reason = "matched " + strings.Join(out.Scores[0].Matches, ", ")
	} else if fallback != "" {
		out.Best, out.Reason = fallback, "no profile signals found; using the fallback profile"
	} else if len(reg.Order) > 0 {
		out.Reason = "no profile signals found"
	}
	return out
}

// Harnesses returns harnesses whose markers exist in the workspace root.
func Harnesses(root string) []string {
	var out []string
	for _, h := range harness.All {
		for _, m := range h.Markers {
			if _, err := os.Stat(filepath.Join(root, m)); err == nil {
				out = append(out, h.ID)
				break
			}
		}
	}
	return out
}

type Recommendation struct {
	PackID string `json:"packId"`
	Reason string `json:"reason"`
}

// Capabilities recommends capability packs from path markers and from what
// the workspace manifest already has installed.
func Capabilities(idx *Index, profile *registry.Profile, installed *model.Manifest) map[string]string {
	out := map[string]string{}
	have := map[string]bool{}
	if installed != nil {
		for _, c := range installed.Components {
			have[c.ID] = true
			if c.Capability != nil && c.Capability.ID != "" {
				have[c.Capability.ID] = true
			}
		}
	}
	for _, g := range profile.CapabilityPackGroups {
		for _, p := range g.Packs {
			if have[p.ID] {
				out[p.ID] = "already installed"
				continue
			}
			if p.RecommendWhen == nil {
				continue
			}
			for _, pat := range p.RecommendWhen.Paths {
				if hit, ok := idx.Match(pat); ok {
					out[p.ID] = "found " + hit
					break
				}
			}
		}
	}
	return out
}

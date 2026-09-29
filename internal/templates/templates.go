// Package templates manages the templates root: init from git, update, and
// validation.
package templates

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/registry"
)

var gitURL = regexp.MustCompile(`^(https?://|ssh://|git@|git://|file://)|\.git$`)

func isGitSource(src string) bool {
	if gitURL.MatchString(src) {
		return true
	}
	_, err := os.Stat(filepath.Join(src, ".git"))
	return err == nil
}

func nonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// Init populates dest from a git URL/repo (clone) or a plain directory (copy).
func Init(src, dest string, force bool) (string, error) {
	if nonEmpty(dest) {
		if !force {
			return "", fmt.Errorf("templates root %s is not empty (use --force to replace it, or `ocws templates update`)", dest)
		}
		backup := dest + ".bak"
		os.RemoveAll(backup)
		if err := os.Rename(dest, backup); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if isGitSource(src) {
		cmd := exec.Command("git", "clone", "--quiet", src, dest)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git clone %s: %w", src, err)
		}
		return "cloned", nil
	}
	if err := CopyTree(src, dest); err != nil {
		return "", err
	}
	return "copied", nil
}

// Update fast-forwards a git templates root.
func Update(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return "", fmt.Errorf("%s is not a git checkout; re-run `ocws init --from <git-url>` to track a repo", root)
	}
	out, err := exec.Command("git", "-C", root, "pull", "--ff-only").CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git pull: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func CopyTree(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dest, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			info, err := d.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
	})
}

type Issue struct {
	Level   string `json:"level"`
	Where   string `json:"where"`
	Message string `json:"message"`
}

// Validate checks every profile reference, pack, harness target, source, and
// render in a templates root.
func Validate(reg *registry.Registry) []Issue {
	var issues []Issue
	add := func(level, where, format string, a ...any) {
		issues = append(issues, Issue{level, where, fmt.Sprintf(format, a...)})
	}
	fileExists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	packOwner := map[string]string{}
	for _, id := range reg.Order {
		p := reg.Profiles[id]
		where := "profile " + id
		for label, ref := range map[string]string{"guide": p.Guide, "starterFilePack": p.StarterFilePack} {
			if ref != "" && !fileExists(reg.Resolve(ref)) {
				add("error", where, "%s %s does not exist", label, ref)
			}
		}
		for h, ref := range p.WorkspaceConfig {
			if _, ok := harness.Get(h); !ok {
				add("error", where, "workspaceConfig targets unknown harness %s", h)
			}
			if !fileExists(reg.Resolve(ref)) {
				add("error", where, "workspaceConfig %s does not exist", ref)
			}
		}
		if p.Detect == nil {
			add("warn", where, "no detect block; the profile can only be chosen manually")
		}
		if p.Scaffold != nil {
			for _, f := range p.Scaffold.Files {
				if !fileExists(reg.Resolve(f.Source)) {
					add("error", where, "scaffold source %s does not exist", f.Source)
				}
			}
		}
		var manifests []string
		for _, b := range p.BasePacks {
			manifests = append(manifests, b.Manifest)
		}
		if p.StarterFilePack != "" {
			manifests = append(manifests, p.StarterFilePack)
		}
		for _, g := range p.CapabilityPackGroups {
			switch g.SelectionMode {
			case "", "multi", "zero-or-one", "single", "exactly-one":
			default:
				add("error", where, "group %s has unknown selectionMode %s", g.ID, g.SelectionMode)
			}
			for _, pk := range g.Packs {
				manifests = append(manifests, pk.Manifest)
			}
		}
		if err := registry.ValidateCapabilitySelection(p, defaultsOf(p)); err != nil {
			add("error", where, "default capability selection: %v", err)
		}
		for _, m := range manifests {
			path := reg.Resolve(m)
			pm, err := registry.LoadPack(path)
			if err != nil {
				add("error", where, "%v", err)
				continue
			}
			if prev, ok := packOwner[pm.ID]; ok && prev != path {
				add("error", "pack "+pm.ID, "id is also used by %s", prev)
			}
			packOwner[pm.ID] = path
			issues = append(issues, validatePack(pm)...)
		}
	}
	return issues
}

func defaultsOf(p *registry.Profile) []string {
	var out []string
	for _, g := range p.CapabilityPackGroups {
		out = append(out, g.Defaults()...)
	}
	return out
}

func validatePack(pm *registry.PackManifest) []Issue {
	var issues []Issue
	where := "pack " + pm.ID
	for _, h := range pm.SupportedHarnesses() {
		files, err := pm.FilesFor(h)
		if err != nil {
			issues = append(issues, Issue{"error", where, err.Error()})
			continue
		}
		seen := map[string]bool{}
		for _, f := range files {
			src := filepath.Join(pm.Dir, f.Source)
			if filepath.IsAbs(f.Source) {
				src = f.Source
			}
			if _, err := os.Stat(src); err != nil {
				issues = append(issues, Issue{"error", where, fmt.Sprintf("[%s] source %s does not exist", h, f.Source)})
				continue
			}
			if f.InstallMode == "" || f.InstallMode == "copy" {
				if seen[f.Destination] {
					issues = append(issues, Issue{"error", where, fmt.Sprintf("[%s] destination %s is listed twice", h, f.Destination)})
				}
				seen[f.Destination] = true
			}
			if f.Render != "" {
				body, _ := os.ReadFile(src)
				header, err := os.ReadFile(filepath.Join(pm.Dir, f.Header))
				if err != nil {
					issues = append(issues, Issue{"error", where, fmt.Sprintf("[%s] header %s does not exist", h, f.Header)})
					continue
				}
				if _, err := harness.Render(f.Render, body, header); err != nil {
					issues = append(issues, Issue{"error", where, fmt.Sprintf("[%s] render %s: %v", h, f.Source, err)})
				}
			}
			if h != "opencode" && strings.HasPrefix(f.Destination, ".opencode/") {
				issues = append(issues, Issue{"warn", where, fmt.Sprintf("[%s] destination %s is inside .opencode/", h, f.Destination)})
			}
		}
	}
	return issues
}

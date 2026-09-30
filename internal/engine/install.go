package engine

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
	"github.com/pelletier/go-toml/v2"
)

var OverwritePolicies = []string{"missing-only", "safe-refresh", "overwrite-approved"}

type InstallOptions struct {
	OverwritePolicy           string
	DryRun                    bool
	AllowExternalDestinations bool
	PruneStaleManaged         bool
}

type InstallFile struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Managed     bool   `json:"managed"`
	Role        string `json:"role,omitempty"`
	InstallMode string `json:"installMode"`
	Status      string `json:"status"`
	Note        string `json:"note,omitempty"`
}

type InstallComponent struct {
	Harness       string            `json:"harness"`
	ComponentType string            `json:"componentType"`
	ID            string            `json:"id"`
	ProfileID     string            `json:"profileId,omitempty"`
	Capability    *model.Capability `json:"capability,omitempty"`
	Files         []InstallFile     `json:"files"`
}

type InstallResult struct {
	ManifestPath    string             `json:"manifestPath"`
	ManifestStatus  ManifestStatus     `json:"manifestStatus"`
	OverwritePolicy string             `json:"overwritePolicy"`
	DryRun          bool               `json:"dryRun"`
	Summary         map[string]int     `json:"summary"`
	Components      []InstallComponent `json:"components"`
}

// Blocked reports whether any file was blocked; write must not follow.
func (r *InstallResult) Blocked() bool {
	for k, v := range r.Summary {
		if strings.HasPrefix(k, "blocked-") && v > 0 {
			return true
		}
	}
	return false
}

type decision struct {
	status string
	note   string
	copy   bool
}

func decide(policy string, hasPrev bool, srcSha string, destExists bool, destSha, prevInstalled, prevSource string) decision {
	if !destExists {
		note := "Destination is missing and will be installed from source."
		if hasPrev {
			note = "Destination is missing and will be repaired from source."
		}
		return decision{"copied", note, true}
	}
	if destSha == srcSha {
		if hasPrev {
			return decision{"skipped-up-to-date", "Destination already matches the current source file.", false}
		}
		return decision{"adopted-existing", "Existing destination already matches the current source file and can be recorded without copying.", false}
	}
	if !hasPrev || prevInstalled == "" {
		if policy == "overwrite-approved" {
			return decision{"copied", "Destination differs from source and overwrite-approved policy allows replacing it.", true}
		}
		return decision{"blocked-conflict", "Destination exists, differs from source, and is not tracked as a safely refreshable managed file.", false}
	}
	if destSha == prevInstalled {
		if policy == "missing-only" {
			return decision{"blocked-conflict", "Source changed since the last install, but missing-only policy does not refresh existing files.", false}
		}
		note := "Destination matches the last installed file and will be refreshed from the newer source."
		if srcSha == prevInstalled {
			note = "Destination still matches the last installed file and will be recopied by policy."
		}
		return decision{"copied", note, true}
	}
	known := srcSha == prevInstalled || (prevSource != "" && srcSha == prevSource)
	if policy == "overwrite-approved" {
		note := "Source and destination both differ from the last installed state, but overwrite-approved policy allows replacing it."
		if known {
			note = "Destination has local changes, but overwrite-approved policy allows replacing it."
		}
		return decision{"copied", note, true}
	}
	note := "Destination has local changes and source also changed; explicit overwrite approval is required."
	if known {
		note = "Destination has local changes and requires explicit overwrite approval."
	}
	return decision{"blocked-conflict", note, false}
}

// Install copies, renders, or merges planned component files.
func (e *Engine) Install(components []model.ComponentPlan, opts InstallOptions) (*InstallResult, error) {
	if opts.OverwritePolicy == "" {
		opts.OverwritePolicy = "safe-refresh"
	}
	if !contains(OverwritePolicies, opts.OverwritePolicy) {
		return nil, fmt.Errorf("unknown overwrite policy %q", opts.OverwritePolicy)
	}
	mr := e.ReadManifest()
	if mr.Status == StatusInvalid && !opts.DryRun {
		return nil, fmt.Errorf("existing workspace setup manifest is invalid: %s. Repair or rebuild it before installing managed files", mr.Error)
	}
	existing := indexComponents(mr.Manifest)
	res := &InstallResult{ManifestPath: mr.Path, ManifestStatus: mr.Status, OverwritePolicy: opts.OverwritePolicy, DryRun: opts.DryRun, Summary: map[string]int{}}
	keep := owned{}
	for _, comp := range components {
		keep.addPlan(e, comp)
	}

	for _, comp := range components {
		prevComp := existing[comp.Key()]
		prevFiles := indexFiles(prevComp)
		planned := map[string]bool{}
		out := InstallComponent{Harness: comp.Harness, ComponentType: comp.ComponentType, ID: comp.ID, ProfileID: comp.ProfileID, Capability: comp.Capability}
		if out.ProfileID == "" && prevComp != nil {
			out.ProfileID = prevComp.ProfileID
		}
		if out.Capability == nil && prevComp != nil {
			out.Capability = prevComp.Capability
		}
		add := func(f InstallFile) {
			res.Summary[f.Status]++
			out.Files = append(out.Files, f)
		}

		for _, f := range comp.Files {
			destPath := e.resolveDest(f.Destination)
			destRel := e.workspaceRel(destPath)
			planned[destRel] = true
			prev := prevFiles[destRel]
			mode := f.InstallMode
			if mode == "" && prev != nil {
				mode = prev.InstallMode
			}
			if mode == "" {
				mode = "copy"
			}
			rec := InstallFile{Source: f.Source, Destination: destRel, Managed: f.IsManaged(), Role: f.Role, InstallMode: mode}

			if !opts.AllowExternalDestinations && !e.withinWorkspace(destPath) {
				rec.Status, rec.Note = "blocked-external-destination", "Destination resolves outside the workspace: "+destPath
				add(rec)
				continue
			}

			src := e.loadSource(comp.SourceRoot, comp.SourceManifest, f)
			if structured(mode) {
				switch mode {
				case "merge", "toml-merge":
					if !src.exists {
						rec.Status, rec.Note = "blocked-missing-source", missingNote(comp, f, src.path)
					} else if opts.DryRun {
						rec.Status, rec.Note = "dry-run-merge", fmt.Sprintf("installMode=%s would merge fragment into %s.", mode, destRel)
					} else if src.err != nil {
						rec.Status, rec.Note = "blocked-render-error", src.err.Error()
					} else if err := mergeFile(mode, src.content(), destPath, f.JSONPointers); err != nil {
						rec.Status, rec.Note = "blocked-invalid-merge", err.Error()
					} else {
						rec.Status = "merged"
						rec.Note = "Merged fragment into " + destRel
						if len(f.JSONPointers) > 0 {
							rec.Note += " at " + strings.Join(f.JSONPointers, ", ")
						}
						rec.Note += "."
					}
				default:
					rec.Status = "skipped-structured-install"
					rec.Note = fmt.Sprintf("installMode=%s requires a structured content edit and is not handled by ocws.", mode)
				}
				add(rec)
				continue
			}

			if !src.exists {
				rec.Status, rec.Note = "blocked-missing-source", missingNote(comp, f, src.path)
				add(rec)
				continue
			}
			if src.err != nil {
				rec.Status, rec.Note = "blocked-render-error", src.err.Error()
				add(rec)
				continue
			}
			destSha, destExists := hashIfExists(destPath)
			var prevInstalled, prevSource string
			if prev != nil {
				prevInstalled, prevSource = prev.InstalledSha256, prev.SourceSha256
			}
			d := decide(opts.OverwritePolicy, prev != nil, src.sha, destExists, destSha, prevInstalled, prevSource)
			rec.Status, rec.Note = d.status, d.note
			if d.copy {
				if opts.DryRun {
					rec.Status = "dry-run-copy"
				} else {
					var err error
					if src.rendered != nil {
						err = jsonx.WriteFileAtomic(destPath, src.rendered, 0o644)
					} else {
						err = copyPathAtomic(src.path, destPath)
					}
					if err != nil {
						return nil, fmt.Errorf("install %s: %w", destRel, err)
					}
					rec.Status = "copied"
				}
			}
			add(rec)
		}

		if opts.PruneStaleManaged && prevComp != nil {
			for _, pf := range prevComp.Files {
				if planned[pf.Destination] {
					continue
				}
				rec, err := e.removeRecorded(prevComp, pf, opts, keep, "-stale")
				if err != nil {
					return nil, err
				}
				add(rec)
			}
		}
		res.Components = append(res.Components, out)
	}
	return res, nil
}

func missingNote(comp model.ComponentPlan, f model.FilePlan, path string) string {
	if path == "" {
		return fmt.Sprintf("Component %s cannot resolve source path for %s", comp.ID, f.Source)
	}
	return "Source path does not exist: " + path
}

func copyPathAtomic(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", dest, os.Getpid())
	os.RemoveAll(tmp)
	if st.IsDir() {
		err = copyDir(src, tmp)
	} else {
		err = copyFile(src, tmp, st.Mode().Perm())
	}
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func copyFile(src, dest string, perm fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, perm); err != nil {
		return err
	}
	return os.Chmod(dest, perm)
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
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
			return copyFile(p, target, info.Mode().Perm())
		}
	})
}

func mergeFile(mode string, source []byte, dest string, pointers []string) error {
	if source == nil {
		return fmt.Errorf("fragment source is unreadable")
	}
	var destData []byte
	var err error
	if exists(dest) {
		if destData, err = os.ReadFile(dest); err != nil {
			return err
		}
	}
	var out []byte
	if mode == "merge" {
		out, err = jsonx.MergeFragmentBytes(source, destData, pointers)
	} else {
		out, err = MergeTOMLFragment(source, destData)
	}
	if err != nil {
		return err
	}
	if destData != nil && string(out) == string(destData) {
		return nil
	}
	return jsonx.WriteFileAtomic(dest, out, 0o644)
}

// MergeTOMLFragment appends a TOML fragment to a destination without
// re-serializing it, so comments and formatting survive. Tables the fragment
// defines must be absent (appended) or already identical (no-op).
func MergeTOMLFragment(fragment, dest []byte) ([]byte, error) {
	var frag map[string]any
	if err := toml.Unmarshal(fragment, &frag); err != nil {
		return nil, fmt.Errorf("parse TOML fragment: %w", err)
	}
	cur := map[string]any{}
	if dest != nil {
		if err := toml.Unmarshal(dest, &cur); err != nil {
			return nil, fmt.Errorf("parse TOML destination: %w", err)
		}
	}
	leaves := tomlLeaves(frag, nil)
	present, differ := 0, []string{}
	for _, leaf := range leaves {
		v, ok := lookup(cur, leaf.path)
		if !ok {
			continue
		}
		present++
		if !reflect.DeepEqual(v, leaf.value) {
			differ = append(differ, strings.Join(leaf.path, "."))
		}
	}
	if len(differ) > 0 {
		return nil, fmt.Errorf("destination already defines %s with different values; edit it by hand or remove it first", strings.Join(differ, ", "))
	}
	if present == len(leaves) {
		return dest, nil
	}
	if present > 0 {
		return nil, fmt.Errorf("destination partially defines the fragment's tables; remove them and re-run")
	}
	var b strings.Builder
	b.Write(dest)
	if len(dest) > 0 {
		if !strings.HasSuffix(string(dest), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.Write(fragment)
	if !strings.HasSuffix(string(fragment), "\n") {
		b.WriteString("\n")
	}
	var check map[string]any
	if err := toml.Unmarshal([]byte(b.String()), &check); err != nil {
		return nil, fmt.Errorf("merged TOML would be invalid: %w", err)
	}
	return []byte(b.String()), nil
}

type tomlLeaf struct {
	path  []string
	value any
}

// tomlLeaves returns the deepest tables (or values) defined by a fragment,
// e.g. mcp_servers.jupyter rather than mcp_servers.
func tomlLeaves(m map[string]any, prefix []string) []tomlLeaf {
	var out []tomlLeaf
	for k, v := range m {
		path := append(append([]string{}, prefix...), k)
		if sub, ok := v.(map[string]any); ok && len(prefix) == 0 {
			out = append(out, tomlLeaves(sub, path)...)
			continue
		}
		out = append(out, tomlLeaf{path, v})
	}
	return out
}

func lookup(m map[string]any, path []string) (any, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

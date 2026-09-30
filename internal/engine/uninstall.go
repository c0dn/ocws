package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
	"github.com/pelletier/go-toml/v2"
)

// owned marks a destination (or dest#pointer for JSON merges) that must
// survive a removal because another component still claims it.
type owned map[string]bool

func (o owned) addPlan(e *Engine, c model.ComponentPlan) {
	for _, f := range c.Files {
		o.add(e.workspaceRel(e.resolveDest(f.Destination)), f.InstallMode, f.JSONPointers)
	}
}

func (o owned) add(dest, mode string, pointers []string) {
	if mode != "merge" {
		o[dest] = true
		return
	}
	for _, p := range pointers {
		o[dest+"#"+p] = true
	}
}

// removeRecorded deletes (or unmerges) one previously installed file. suffix
// distinguishes statuses, e.g. "-stale" for files a pack stopped shipping.
func (e *Engine) removeRecorded(comp *model.ComponentRecord, pf model.FileRecord, opts InstallOptions, keep owned, suffix string) (InstallFile, error) {
	destPath := e.resolveDest(pf.Destination)
	mode := pf.InstallMode
	if mode == "" {
		mode = "copy"
	}
	rec := InstallFile{Source: pf.Source, Destination: pf.Destination, Managed: pf.Managed, Role: pf.Role, InstallMode: mode}
	set := func(status, note string) (InstallFile, error) {
		rec.Status, rec.Note = status, note
		return rec, nil
	}
	if !opts.AllowExternalDestinations && !e.withinWorkspace(destPath) {
		return set("blocked-external-destination", "Destination resolves outside the workspace: "+destPath)
	}
	if !exists(destPath) {
		return set("already-absent"+suffix, "Destination is already absent.")
	}
	force := opts.OverwritePolicy == "overwrite-approved"

	switch mode {
	case "copy":
		if keep[pf.Destination] {
			return set("kept-shared"+suffix, "Another installed component still manages this destination.")
		}
		destSha, _ := hashIfExists(destPath)
		if destSha != pf.InstalledSha256 && !force {
			return set("blocked"+suffix+"-conflict", "Destination has local changes; use --overwrite overwrite-approved to remove it anyway.")
		}
		if opts.DryRun {
			return set("dry-run-remove"+suffix, "Destination would be removed.")
		}
		if err := os.RemoveAll(destPath); err != nil {
			return rec, err
		}
		e.removeEmptyParents(destPath)
		return set("removed"+suffix, "Destination was removed.")

	case "merge":
		var pointers []string
		for _, p := range pf.JSONPointers {
			if !keep[pf.Destination+"#"+p] {
				pointers = append(pointers, p)
			}
		}
		if len(pointers) == 0 {
			return set("kept-shared"+suffix, "Another installed component still merges the same keys.")
		}
		var fragment []byte
		if comp != nil {
			src := e.loadSource(comp.SourceRoot, comp.SourceManifest, model.FilePlan{Source: pf.Source, Destination: pf.Destination, Render: pf.Render, Header: pf.Header})
			if src.exists && src.err == nil {
				fragment = src.content()
			}
		}
		cur, err := os.ReadFile(destPath)
		if err != nil {
			return rec, err
		}
		out, err := jsonx.UnmergeFragmentBytes(fragment, cur, pointers, pf.PointerSha256, force)
		if err != nil {
			return set("blocked"+suffix+"-conflict", fmt.Sprintf("Cannot unmerge %s from %s: %v; use --overwrite overwrite-approved or edit it by hand.", strings.Join(pointers, ", "), pf.Destination, err))
		}
		if string(out) == string(cur) {
			return set("already-absent"+suffix, "Merged keys are already absent.")
		}
		if opts.DryRun {
			return set("dry-run-unmerge"+suffix, fmt.Sprintf("Would remove %s from %s.", strings.Join(pointers, ", "), pf.Destination))
		}
		if err := jsonx.WriteFileAtomic(destPath, out, 0o644); err != nil {
			return rec, err
		}
		return set("unmerged"+suffix, fmt.Sprintf("Removed %s from %s.", strings.Join(pointers, ", "), pf.Destination))

	case "toml-merge", "text-merge":
		// Fragments are appended verbatim, so remove that exact text.
		var fragment []byte
		if comp != nil {
			src := e.loadSource(comp.SourceRoot, comp.SourceManifest, model.FilePlan{Source: pf.Source, Destination: pf.Destination, Render: pf.Render, Header: pf.Header})
			if src.exists && src.err == nil {
				fragment = src.content()
			}
		}
		cur, err := os.ReadFile(destPath)
		if err != nil {
			return rec, err
		}
		frag := strings.TrimSpace(string(fragment))
		if frag == "" || !strings.Contains(string(cur), frag) {
			return set("left-merged"+suffix, fmt.Sprintf("The merged block no longer matches the pack's fragment; delete it from %s by hand.", pf.Destination))
		}
		out := strings.Replace(string(cur), frag, "", 1)
		if mode == "toml-merge" {
			data, err := UnmergeTOMLFragment(fragment, cur)
			if err != nil {
				return set("blocked"+suffix+"-conflict", fmt.Sprintf("Cannot safely unmerge TOML from %s: %v; edit it by hand.", pf.Destination, err))
			}
			out = string(data)
		}
		if mode == "text-merge" {
			for strings.Contains(out, "\n\n\n") {
				out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
			}
			out = strings.TrimLeft(out, "\n")
		}
		if opts.DryRun {
			return set("dry-run-unmerge"+suffix, "Merged block would be removed from "+pf.Destination+".")
		}
		if strings.TrimSpace(out) == "" {
			if err := os.Remove(destPath); err != nil {
				return rec, err
			}
			e.removeEmptyParents(destPath)
			return set("unmerged"+suffix, "Removed "+pf.Destination+" (it only held the merged fragment).")
		}
		if err := jsonx.WriteFileAtomic(destPath, []byte(out), 0o644); err != nil {
			return rec, err
		}
		return set("unmerged"+suffix, "Removed the merged block from "+pf.Destination+".")

	default:
		return set("left-merged"+suffix, fmt.Sprintf("installMode=%s content cannot be removed automatically; delete it from %s by hand.", mode, pf.Destination))
	}
}

// UnmergeTOMLFragment preserves the original formatting, but verifies the
// parsed tables before and after removal. A matching text block alone is not
// sufficient: deleting its header could reparent user-added settings.
func UnmergeTOMLFragment(fragment, dest []byte) ([]byte, error) {
	var frag, cur, after map[string]any
	if err := toml.Unmarshal(fragment, &frag); err != nil {
		return nil, fmt.Errorf("parse TOML fragment: %w", err)
	}
	if err := toml.Unmarshal(dest, &cur); err != nil {
		return nil, fmt.Errorf("parse TOML destination: %w", err)
	}
	leaves := tomlLeaves(frag, nil)
	for _, leaf := range leaves {
		v, ok := lookup(cur, leaf.path)
		if !ok || !reflect.DeepEqual(v, leaf.value) {
			return nil, fmt.Errorf("table or value %s has local changes", strings.Join(leaf.path, "."))
		}
	}
	block := strings.TrimSpace(string(fragment))
	if block == "" || !strings.Contains(string(dest), block) {
		return nil, fmt.Errorf("destination no longer contains the original fragment")
	}
	out := []byte(strings.Replace(string(dest), block, "", 1))
	if err := toml.Unmarshal(out, &after); err != nil {
		return nil, fmt.Errorf("removal would produce invalid TOML: %w", err)
	}
	for _, leaf := range leaves {
		if _, ok := lookup(after, leaf.path); ok {
			return nil, fmt.Errorf("removal would leave %s behind", strings.Join(leaf.path, "."))
		}
	}
	// Check both directions so no user settings disappear or acquire a new
	// scope, including when a matching block occurs inside a multiline string.
	for _, pair := range [][2]map[string]any{{cur, after}, {after, cur}} {
		for _, leaf := range tomlLeaves(pair[0], nil) {
			if _, owned := lookup(frag, leaf.path); owned {
				continue
			}
			v, ok := lookup(pair[1], leaf.path)
			if !ok || !reflect.DeepEqual(v, leaf.value) {
				return nil, fmt.Errorf("removal would change user setting %s", strings.Join(leaf.path, "."))
			}
		}
	}
	return out, nil
}

// removeEmptyParents drops directories left empty by a removal, stopping at
// the workspace root.
func (e *Engine) removeEmptyParents(p string) {
	for dir := filepath.Dir(p); e.withinWorkspace(dir) && dir != e.Workspace; dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			return
		}
	}
}

// ResolveInstalled maps component ids ("id" or "harness:id") to manifest keys.
func (e *Engine) ResolveInstalled(ids []string) ([]string, error) {
	mr := e.ReadManifest()
	if mr.Status != StatusOK {
		return nil, fmt.Errorf("no readable ocws manifest at %s", mr.Path)
	}
	var keys, unknown []string
	seen := map[string]bool{}
	for _, id := range ids {
		found := false
		for _, c := range mr.Manifest.Components {
			if id == c.Key() || id == c.ID {
				found = true
				if !seen[c.Key()] {
					seen[c.Key()] = true
					keys = append(keys, c.Key())
				}
			}
		}
		if !found {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		var have []string
		for _, c := range mr.Manifest.Components {
			have = append(have, c.Key())
		}
		sort.Strings(have)
		return nil, fmt.Errorf("not installed: %s (installed: %s)", strings.Join(unknown, ", "), strings.Join(have, ", "))
	}
	return keys, nil
}

// Uninstall removes the files of installed components (manifest keys).
// Destinations still claimed by other manifest components or by keep are
// left alone. The manifest itself is updated by Write(RemoveKeys).
func (e *Engine) Uninstall(keys []string, keep []model.ComponentPlan, opts InstallOptions) (*InstallResult, error) {
	if opts.OverwritePolicy == "" {
		opts.OverwritePolicy = "safe-refresh"
	}
	if !contains(OverwritePolicies, opts.OverwritePolicy) {
		return nil, fmt.Errorf("unknown overwrite policy %q", opts.OverwritePolicy)
	}
	mr := e.ReadManifest()
	if mr.Status != StatusOK {
		return nil, fmt.Errorf("no readable ocws manifest at %s", mr.Path)
	}
	removing := map[string]bool{}
	for _, k := range keys {
		removing[k] = true
	}
	claimed := owned{}
	for _, c := range mr.Manifest.Components {
		if !removing[c.Key()] {
			for _, f := range c.Files {
				claimed.add(f.Destination, f.InstallMode, f.JSONPointers)
			}
		}
	}
	for _, c := range keep {
		claimed.addPlan(e, c)
	}
	existing := indexComponents(mr.Manifest)
	res := &InstallResult{ManifestPath: mr.Path, ManifestStatus: mr.Status, OverwritePolicy: opts.OverwritePolicy, DryRun: opts.DryRun, Summary: map[string]int{}}
	for _, k := range keys {
		comp := existing[k]
		if comp == nil {
			return nil, fmt.Errorf("component %s is not in the manifest", k)
		}
		out := InstallComponent{Harness: comp.HarnessID(), ComponentType: comp.ComponentType, ID: comp.ID, ProfileID: comp.ProfileID, Capability: comp.Capability}
		for _, pf := range comp.Files {
			rec, err := e.removeRecorded(comp, pf, opts, claimed, "")
			if err != nil {
				return nil, fmt.Errorf("remove %s: %w", pf.Destination, err)
			}
			res.Summary[rec.Status]++
			out.Files = append(out.Files, rec)
		}
		res.Components = append(res.Components, out)
	}
	return res, nil
}

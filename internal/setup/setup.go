// Package setup orchestrates a full workspace setup run: plan, audit, install,
// workspace config, instructions, scaffold, and manifest write.
package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/c0dn/ocws/internal/engine"
	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
	"github.com/c0dn/ocws/internal/registry"
)

// Modes for files ocws generates or merges outside pack management.
const (
	ModeMerge   = "merge"
	ModeReplace = "replace"
	ModeSkip    = "skip"
	ModeCreate  = "create" // create only when missing
)

type Options struct {
	ProfileID      string
	Harnesses      []string
	BasePackIDs    []string // nil = profile defaults
	CapabilityIDs  []string // nil = profile defaults
	IncludeStarter bool

	Overwrite string
	DryRun    bool
	Prune     bool
	// ForceRemove lets pruning delete edited or unverifiable files of
	// components dropped from the plan, independent of Overwrite.
	ForceRemove bool

	// ConfigMode applies to the profile's workspace config template(s).
	ConfigMode string
	// AgentsMode controls AGENTS.md: create (default), replace, or skip.
	AgentsMode             string
	Agents                 AgentsInfo
	Scaffold               bool
	RebuildInvalidManifest bool
}

type Action struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Note   string `json:"note,omitempty"`
}

type Report struct {
	Plan     *registry.PlanResult  `json:"plan"`
	Audit    *engine.AuditResult   `json:"audit"`
	Install  *engine.InstallResult `json:"install,omitempty"`
	Removed  *engine.InstallResult `json:"removed,omitempty"`
	Write    *engine.WriteResult   `json:"write,omitempty"`
	Actions  []Action              `json:"actions"`
	Warnings []string              `json:"warnings"`
	Notes    []string              `json:"notes"`
}

var ErrBlocked = errors.New("install blocked; manifest not written")

// Preflight describes the workspace before changes.
type Preflight struct {
	Writable      bool     `json:"writable"`
	GitDirty      bool     `json:"gitDirty"`
	ExistingPaths []string `json:"existingPaths"`
}

func CheckPreflight(ws string, harnesses []string) Preflight {
	p := Preflight{ExistingPaths: []string{}}
	if f, err := os.CreateTemp(ws, ".ocws-write-test-*"); err == nil {
		p.Writable = true
		f.Close()
		os.Remove(f.Name())
	}
	if out, err := exec.Command("git", "-C", ws, "status", "--porcelain").Output(); err == nil {
		p.GitDirty = len(strings.TrimSpace(string(out))) > 0
	}
	targets := []string{"AGENTS.md", engine.ManifestRel}
	for _, h := range harnesses {
		hs, _ := harness.Get(h)
		for _, t := range []string{"config", "mcp", "agents", "commands", "skills", "tools"} {
			if v, ok := hs.Tokens[t]; ok && !contains(targets, v) {
				targets = append(targets, v)
			}
		}
		if h == "claude" {
			targets = append(targets, "CLAUDE.md")
		}
	}
	for _, t := range targets {
		if _, err := os.Stat(filepath.Join(ws, t)); err == nil {
			p.ExistingPaths = append(p.ExistingPaths, t)
		}
	}
	return p
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// Plan resolves options into component plans and audits them.
func Plan(eng *engine.Engine, reg *registry.Registry, o Options) (*Report, error) {
	plan, err := reg.Plan(registry.PlanRequest{ProfileID: o.ProfileID, Harnesses: o.Harnesses,
		BasePackIDs: o.BasePackIDs, CapabilityIDs: o.CapabilityIDs, IncludeStarter: o.IncludeStarter})
	if err != nil {
		return nil, err
	}
	r := &Report{Plan: plan, Audit: eng.Audit(plan.Components), Actions: []Action{}, Warnings: []string{}, Notes: []string{}}
	for _, s := range plan.Skipped {
		r.Warnings = append(r.Warnings, fmt.Sprintf("%s not installed for %s: %s", s.PackID, s.Harness, s.Reason))
	}
	return r, nil
}

// Apply runs the full setup. Install is dry-run first so a blocked plan
// changes nothing.
func Apply(eng *engine.Engine, reg *registry.Registry, o Options) (*Report, error) {
	r, err := Plan(eng, reg, o)
	if err != nil {
		return nil, err
	}
	profile, _ := reg.Profile(o.ProfileID)
	installOpts := engine.InstallOptions{OverwritePolicy: o.Overwrite, DryRun: true, PruneStaleManaged: o.Prune}
	pre, err := eng.Install(r.Plan.Components, installOpts)
	if err != nil {
		return r, err
	}
	r.Install = pre
	var dropped []string
	if o.Prune {
		for _, s := range r.Audit.StaleComponents {
			dropped = append(dropped, model.Key(s.Harness, s.ID))
		}
	}
	removeOpts := installOpts
	if o.ForceRemove {
		removeOpts.OverwritePolicy = "overwrite-approved"
	}
	if len(dropped) > 0 {
		un, err := eng.Uninstall(dropped, r.Plan.Components, removeOpts)
		if err != nil {
			return r, err
		}
		r.Removed = un
		if un.Blocked() {
			return r, ErrBlocked
		}
	}
	if pre.Blocked() {
		return r, ErrBlocked
	}
	if o.DryRun {
		r.Actions = append(r.Actions, planGenerated(eng, reg, profile, o)...)
		return r, nil
	}

	acts, warns, err := applyWorkspaceConfig(eng.Workspace, reg, profile, o)
	r.Actions, r.Warnings = append(r.Actions, acts...), append(r.Warnings, warns...)
	if err != nil {
		return r, err
	}

	installOpts.DryRun = false
	res, err := eng.Install(r.Plan.Components, installOpts)
	r.Install = res
	if err != nil {
		return r, err
	}
	if res.Blocked() {
		return r, ErrBlocked
	}
	if len(dropped) > 0 {
		removeOpts.DryRun = false
		un, err := eng.Uninstall(dropped, r.Plan.Components, removeOpts)
		r.Removed = un
		if err != nil {
			return r, err
		}
		if un.Blocked() {
			return r, ErrBlocked
		}
	}

	acts, warns, err = applyInstructions(eng.Workspace, reg, profile, o)
	r.Actions, r.Warnings = append(r.Actions, acts...), append(r.Warnings, warns...)
	if err != nil {
		return r, err
	}
	if o.Scaffold {
		acts, err = applyScaffold(eng.Workspace, reg, profile)
		r.Actions = append(r.Actions, acts...)
		if err != nil {
			return r, err
		}
	}

	w, err := eng.Write(r.Plan.Components, engine.WriteOptions{ProjectType: o.ProfileID, Harnesses: o.Harnesses, RemoveKeys: dropped, RebuildInvalid: o.RebuildInvalidManifest})
	if err != nil {
		return r, err
	}
	r.Write = w
	if !o.Prune {
		var left []string
		for _, c := range r.Audit.Components {
			for _, s := range c.StaleFiles {
				left = append(left, s.Destination)
			}
		}
		if len(left) > 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("packs no longer ship %s; left in place and no longer tracked (use --prune to remove such files)", strings.Join(left, ", ")))
		}
	}
	for _, h := range o.Harnesses {
		hs, _ := harness.Get(h)
		r.Notes = append(r.Notes, hs.Notes...)
	}
	return r, nil
}

func planGenerated(eng *engine.Engine, reg *registry.Registry, profile *registry.Profile, o Options) []Action {
	var out []Action
	for h, p := range profile.WorkspaceConfig {
		if !contains(o.Harnesses, h) || o.ConfigMode == ModeSkip {
			continue
		}
		dest, _ := configDest(h, p)
		out = append(out, Action{Path: dest, Action: "would-" + orDefault(o.ConfigMode, ModeMerge)})
	}
	if o.AgentsMode != ModeSkip {
		out = append(out, Action{Path: "AGENTS.md", Action: "would-" + orDefault(o.AgentsMode, ModeCreate)})
	}
	if o.Scaffold && profile.Scaffold != nil {
		out = append(out, Action{Path: "(scaffold)", Action: "would-create-missing"})
	}
	return out
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// configDest maps a harness config template to its workspace destination.
func configDest(h, templatePath string) (string, error) {
	hs, ok := harness.Get(h)
	if !ok {
		return "", fmt.Errorf("unknown harness %s in workspaceConfig", h)
	}
	dest := hs.Tokens["config"]
	if h == "opencode" && strings.HasSuffix(templatePath, ".jsonc") {
		dest = "opencode.jsonc"
	}
	return dest, nil
}

func applyWorkspaceConfig(ws string, reg *registry.Registry, profile *registry.Profile, o Options) ([]Action, []string, error) {
	var acts []Action
	var warns []string
	mode := orDefault(o.ConfigMode, ModeMerge)
	if mode == ModeSkip {
		return nil, nil, nil
	}
	for _, h := range o.Harnesses {
		tpl, ok := profile.WorkspaceConfig[h]
		if !ok {
			continue
		}
		src := reg.Resolve(tpl)
		rel, err := configDest(h, tpl)
		if err != nil {
			return acts, warns, err
		}
		if h == "opencode" {
			if _, err := os.Stat(filepath.Join(ws, "opencode.jsonc")); err == nil {
				rel = "opencode.jsonc"
			}
		}
		dest := filepath.Join(ws, rel)
		data, err := os.ReadFile(src)
		if err != nil {
			return acts, warns, fmt.Errorf("workspace config template %s: %w", src, err)
		}
		_, statErr := os.Stat(dest)
		if statErr != nil || mode == ModeReplace {
			if err := jsonx.WriteFileAtomic(dest, data, 0o644); err != nil {
				return acts, warns, err
			}
			action := "created"
			if statErr == nil {
				action = "replaced"
			}
			acts = append(acts, Action{Path: rel, Action: action})
			continue
		}
		cur, err := os.ReadFile(dest)
		if err != nil {
			return acts, warns, err
		}
		if strings.HasSuffix(rel, ".toml") {
			out, err := engine.MergeTOMLFragment(data, cur)
			if err != nil {
				warns = append(warns, fmt.Sprintf("%s: not merged (%v)", rel, err))
				acts = append(acts, Action{Path: rel, Action: "kept", Note: err.Error()})
				continue
			}
			if string(out) != string(cur) {
				if err := jsonx.WriteFileAtomic(dest, out, 0o644); err != nil {
					return acts, warns, err
				}
				acts = append(acts, Action{Path: rel, Action: "merged"})
			} else {
				acts = append(acts, Action{Path: rel, Action: "unchanged"})
			}
			continue
		}
		existing, err := jsonx.Parse(cur)
		eo, ok := existing.(*jsonx.Object)
		if err != nil || !ok {
			warns = append(warns, fmt.Sprintf("%s is not plain JSON (comments?); left untouched", rel))
			acts = append(acts, Action{Path: rel, Action: "kept"})
			continue
		}
		incoming, err := jsonx.Parse(data)
		io, ok := incoming.(*jsonx.Object)
		if err != nil || !ok {
			return acts, warns, fmt.Errorf("workspace config template %s must be a JSON object", src)
		}
		conflicts := jsonx.FillMissing(eo, io, "")
		out := append(jsonx.Marshal(eo), '\n')
		if string(out) != string(cur) {
			if err := jsonx.WriteFileAtomic(dest, out, 0o644); err != nil {
				return acts, warns, err
			}
			acts = append(acts, Action{Path: rel, Action: "merged", Note: conflictNote(conflicts)})
		} else {
			acts = append(acts, Action{Path: rel, Action: "unchanged", Note: conflictNote(conflicts)})
		}
		if len(conflicts) > 0 {
			warns = append(warns, fmt.Sprintf("%s kept your values for %s (template differs; use --config replace to take the template)", rel, strings.Join(conflicts, ", ")))
		}
	}
	return acts, warns, nil
}

func conflictNote(c []string) string {
	if len(c) == 0 {
		return ""
	}
	return "kept existing: " + strings.Join(c, ", ")
}

func applyScaffold(ws string, reg *registry.Registry, profile *registry.Profile) ([]Action, error) {
	if profile.Scaffold == nil {
		return nil, nil
	}
	var acts []Action
	for _, d := range profile.Scaffold.Dirs {
		p := filepath.Join(ws, filepath.FromSlash(d))
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return acts, err
		}
		acts = append(acts, Action{Path: d, Action: "created-dir"})
	}
	for _, f := range profile.Scaffold.Files {
		dest := filepath.Join(ws, filepath.FromSlash(f.Path))
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		data, err := os.ReadFile(reg.Resolve(f.Source))
		if err != nil {
			return acts, fmt.Errorf("scaffold source %s: %w", f.Source, err)
		}
		if err := jsonx.WriteFileAtomic(dest, data, 0o644); err != nil {
			return acts, err
		}
		acts = append(acts, Action{Path: f.Path, Action: "created"})
	}
	return acts, nil
}

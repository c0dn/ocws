// Package registry loads the template registry (profiles.json) and pack
// manifests, and resolves profile selections into per-harness component plans.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
)

// RegistryFiles are checked in order inside a templates root.
var RegistryFiles = []string{"profiles.json"}

type Registry struct {
	Path          string
	Root          string
	SchemaVersion int
	Profiles      map[string]*Profile
	Order         []string
}

type Detect struct {
	Paths    []string `json:"paths,omitempty"`
	Priority int      `json:"priority,omitempty"`
	Fallback bool     `json:"fallback,omitempty"`
}

type ScaffoldFile struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type Scaffold struct {
	Dirs  []string       `json:"dirs,omitempty"`
	Files []ScaffoldFile `json:"files,omitempty"`
}

type BasePack struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName,omitempty"`
	ComponentType   string `json:"componentType,omitempty"`
	Manifest        string `json:"manifest"`
	DefaultSelected *bool  `json:"defaultSelected,omitempty"`
}

func (b BasePack) IsDefault() bool { return b.DefaultSelected == nil || *b.DefaultSelected }

type Recommend struct {
	Paths []string `json:"paths,omitempty"`
}

type CapabilityPack struct {
	ID              string     `json:"id"`
	DisplayName     string     `json:"displayName,omitempty"`
	ComponentType   string     `json:"componentType,omitempty"`
	Manifest        string     `json:"manifest"`
	DefaultSelected bool       `json:"defaultSelected,omitempty"`
	RecommendWhen   *Recommend `json:"recommendWhen,omitempty"`
}

type CapabilityGroup struct {
	ID              string           `json:"id"`
	DisplayName     string           `json:"displayName,omitempty"`
	Description     string           `json:"description,omitempty"`
	SelectionMode   string           `json:"selectionMode,omitempty"`
	DefaultSelected *[]string        `json:"defaultSelected,omitempty"`
	Packs           []CapabilityPack `json:"packs,omitempty"`
}

// Defaults returns the group's default capability selection.
func (g CapabilityGroup) Defaults() []string {
	if g.DefaultSelected != nil {
		return *g.DefaultSelected
	}
	var out []string
	for _, p := range g.Packs {
		if p.DefaultSelected {
			out = append(out, p.ID)
		}
	}
	return out
}

// WorkspaceConfig maps harness id -> config template path. A plain string in
// the registry means the OpenCode config.
type WorkspaceConfig map[string]string

func (w *WorkspaceConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*w = WorkspaceConfig{"opencode": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("workspaceConfig must be a string or harness map: %w", err)
	}
	*w = m
	return nil
}

type Profile struct {
	ID                   string            `json:"-"`
	DisplayName          string            `json:"displayName,omitempty"`
	Description          string            `json:"description,omitempty"`
	Guide                string            `json:"guide,omitempty"`
	WorkspaceConfig      WorkspaceConfig   `json:"workspaceConfig,omitempty"`
	StarterFilePack      string            `json:"starterFilePack,omitempty"`
	BasePacks            []BasePack        `json:"basePacks,omitempty"`
	CapabilityPackGroups []CapabilityGroup `json:"capabilityPackGroups,omitempty"`
	Detect               *Detect           `json:"detect,omitempty"`
	Scaffold             *Scaffold         `json:"scaffold,omitempty"`
}

func (p *Profile) Name() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.ID
}

// FindRegistry returns the registry file inside a templates root.
func FindRegistry(root string) (string, error) {
	for _, name := range RegistryFiles {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no %s found in templates root %s (run `ocws init`)", strings.Join(RegistryFiles, " or "), root)
}

func Load(root string) (*Registry, error) {
	path, err := FindRegistry(root)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		SchemaVersion int                        `json:"schemaVersion"`
		Profiles      map[string]json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if raw.SchemaVersion != 3 {
		return nil, fmt.Errorf("registry %s must use schemaVersion 3 (got %d)", path, raw.SchemaVersion)
	}
	if raw.Profiles == nil {
		return nil, fmt.Errorf("registry %s is missing a profiles object", path)
	}
	reg := &Registry{Path: path, Root: filepath.Dir(path), SchemaVersion: raw.SchemaVersion, Profiles: map[string]*Profile{}}
	ordered, _ := jsonx.Parse(data)
	if obj, ok := ordered.(*jsonx.Object); ok {
		if profs, ok := obj.Values["profiles"].(*jsonx.Object); ok {
			reg.Order = append(reg.Order, profs.Keys...)
		}
	}
	for id, msg := range raw.Profiles {
		p := &Profile{}
		if err := json.Unmarshal(msg, p); err != nil {
			return nil, fmt.Errorf("profile %s: %w", id, err)
		}
		p.ID = id
		reg.Profiles[id] = p
	}
	return reg, nil
}

// Resolve maps a registry-relative asset path to an absolute path.
func (r *Registry) Resolve(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(r.Root, p)
}

func (r *Registry) Profile(id string) (*Profile, error) {
	p, ok := r.Profiles[id]
	if !ok {
		return nil, fmt.Errorf("unknown workspace profile %q (available: %s)", id, strings.Join(r.Order, ", "))
	}
	return p, nil
}

// PackManifest is a template pack manifest (schema 2 or 3).
type PackManifest struct {
	Path          string                `json:"-"`
	Dir           string                `json:"-"`
	SchemaVersion int                   `json:"schemaVersion"`
	ID            string                `json:"id"`
	DisplayName   string                `json:"displayName,omitempty"`
	ComponentType string                `json:"componentType,omitempty"`
	Version       string                `json:"version,omitempty"`
	Description   string                `json:"description,omitempty"`
	Capability    *model.Capability     `json:"capability,omitempty"`
	Harnesses     []string              `json:"harnesses,omitempty"`
	Files         []model.FilePlan      `json:"files,omitempty"`
	Targets       map[string]PackTarget `json:"targets,omitempty"`
}

type PackTarget struct {
	Files []model.FilePlan `json:"files"`
}

func LoadPack(path string) (*PackManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pack manifest: %w", err)
	}
	m := &PackManifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse pack manifest %s: %w", path, err)
	}
	m.Path, m.Dir = path, filepath.Dir(path)
	if m.SchemaVersion != 3 {
		return nil, fmt.Errorf("pack manifest %s must use schemaVersion 3 (got %d)", path, m.SchemaVersion)
	}
	if strings.TrimSpace(m.ID) == "" {
		return nil, fmt.Errorf("pack manifest %s is missing id", path)
	}
	for h := range m.Targets {
		if _, ok := harness.Get(h); !ok {
			return nil, fmt.Errorf("pack %s targets unknown harness %q", m.ID, h)
		}
	}
	for _, h := range m.Harnesses {
		if _, ok := harness.Get(h); !ok {
			return nil, fmt.Errorf("pack %s lists unknown harness %q", m.ID, h)
		}
	}
	return m, nil
}

// SharedHarnesses are the harnesses that receive top-level files.
func (m *PackManifest) SharedHarnesses() []string {
	if len(m.Harnesses) > 0 {
		return m.Harnesses
	}
	if len(m.Files) > 0 {
		return []string{"opencode"}
	}
	return nil
}

// Supports reports whether the pack can be installed for a harness.
func (m *PackManifest) Supports(h string) bool {
	_, ok := m.Targets[h]
	return ok || slices.Contains(m.SharedHarnesses(), h)
}

func (m *PackManifest) SupportedHarnesses() []string {
	var out []string
	for _, h := range harness.IDs() {
		if m.Supports(h) {
			out = append(out, h)
		}
	}
	return out
}

// FilesFor returns the normalized file list for a harness.
func (m *PackManifest) FilesFor(h string) ([]model.FilePlan, error) {
	hs, ok := harness.Get(h)
	if !ok {
		return nil, fmt.Errorf("unknown harness %q", h)
	}
	var files []model.FilePlan
	if slices.Contains(m.SharedHarnesses(), h) {
		files = append(files, m.Files...)
	}
	if t, ok := m.Targets[h]; ok {
		files = append(files, t.Files...)
	}
	out := make([]model.FilePlan, 0, len(files))
	for i, f := range files {
		if strings.TrimSpace(f.Source) == "" || strings.TrimSpace(f.Destination) == "" {
			return nil, fmt.Errorf("pack %s file %d needs source and destination", m.ID, i)
		}
		dest, err := hs.Expand(filepath.ToSlash(f.Destination))
		if err != nil {
			return nil, fmt.Errorf("pack %s file %s: %w", m.ID, f.Source, err)
		}
		f.Destination = dest
		if f.Managed == nil {
			f.Managed = model.BoolPtr(true)
		}
		f.Role = strings.TrimSpace(f.Role)
		f.InstallMode = strings.TrimSpace(f.InstallMode)
		if f.Render != "" && f.Header == "" {
			return nil, fmt.Errorf("pack %s file %s uses render=%s without a header", m.ID, f.Source, f.Render)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pack %s has no files for harness %s", m.ID, h)
	}
	return out, nil
}

// Plan converts the pack into a component plan for one harness.
func (m *PackManifest) Plan(h, profileID, fallbackType string, capability *model.Capability) (model.ComponentPlan, error) {
	files, err := m.FilesFor(h)
	if err != nil {
		return model.ComponentPlan{}, err
	}
	ct := m.ComponentType
	if ct == "" {
		ct = fallbackType
	}
	if !model.ValidComponentType(ct) {
		return model.ComponentPlan{}, fmt.Errorf("pack manifest %s has unsupported componentType %q", m.Path, ct)
	}
	return model.ComponentPlan{
		Harness: h, ComponentType: ct, ID: m.ID, DisplayName: m.DisplayName,
		SourceRoot: m.Dir, SourceManifest: m.Path, Version: m.Version,
		ManifestSchemaVersion: m.SchemaVersion, ProfileID: profileID,
		Capability: model.MergeCapability(m.Capability, capability), Files: files,
	}, nil
}

type PlanRequest struct {
	ProfileID string
	Harnesses []string
	// nil means "profile defaults"; an empty slice selects nothing.
	BasePackIDs    []string
	CapabilityIDs  []string
	IncludeStarter bool
}

type Skip struct {
	PackID  string `json:"packId"`
	Harness string `json:"harness"`
	Reason  string `json:"reason"`
}

type PlanResult struct {
	RegistryPath         string                `json:"registryPath"`
	ProfileID            string                `json:"profileId"`
	ProfileDisplayName   string                `json:"profileDisplayName"`
	Harnesses            []string              `json:"harnesses"`
	SelectedBasePackIDs  []string              `json:"selectedBasePackIds"`
	SelectedCapabilities []string              `json:"selectedCapabilityIds"`
	IncludeStarterFiles  bool                  `json:"includeStarterFiles"`
	Components           []model.ComponentPlan `json:"components"`
	Skipped              []Skip                `json:"skipped,omitempty"`
}

type selectedPack struct {
	manifest     string
	fallbackType string
	capability   *model.Capability
}

// Plan resolves a profile selection into component plans for every harness.
func (r *Registry) Plan(req PlanRequest) (*PlanResult, error) {
	profile, err := r.Profile(req.ProfileID)
	if err != nil {
		return nil, err
	}
	if len(req.Harnesses) == 0 {
		req.Harnesses = []string{"opencode"}
	}
	baseIDs := req.BasePackIDs
	if baseIDs == nil {
		for _, b := range profile.BasePacks {
			if b.IsDefault() {
				baseIDs = append(baseIDs, b.ID)
			}
		}
	}
	var missing []string
	for _, id := range baseIDs {
		if !slices.ContainsFunc(profile.BasePacks, func(b BasePack) bool { return b.ID == id }) {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("profile %s does not define base pack ids: %s", req.ProfileID, strings.Join(missing, ", "))
	}

	capIDs := req.CapabilityIDs
	userCaps := req.CapabilityIDs != nil
	if capIDs == nil {
		for _, g := range profile.CapabilityPackGroups {
			capIDs = append(capIDs, g.Defaults()...)
		}
	}
	if err := ValidateCapabilitySelection(profile, capIDs); err != nil {
		return nil, err
	}

	var packs []selectedPack
	for _, b := range profile.BasePacks {
		if slices.Contains(baseIDs, b.ID) {
			packs = append(packs, selectedPack{manifest: b.Manifest, fallbackType: b.ComponentType})
		}
	}
	if req.IncludeStarter && profile.StarterFilePack != "" {
		packs = append(packs, selectedPack{manifest: profile.StarterFilePack, fallbackType: "template-pack"})
	}
	for _, g := range profile.CapabilityPackGroups {
		for _, p := range g.Packs {
			if !slices.Contains(capIDs, p.ID) {
				continue
			}
			name := p.DisplayName
			if name == "" {
				name = g.DisplayName
			}
			if name == "" {
				name = p.ID
			}
			selectedBy := "default"
			if userCaps {
				selectedBy = "user"
			}
			packs = append(packs, selectedPack{manifest: p.Manifest, fallbackType: p.ComponentType,
				capability: &model.Capability{ID: p.ID, GroupID: g.ID, DisplayName: name, Optional: model.BoolPtr(true), SelectedBy: selectedBy}})
		}
	}

	res := &PlanResult{RegistryPath: r.Path, ProfileID: profile.ID, ProfileDisplayName: profile.Name(), Harnesses: req.Harnesses,
		SelectedBasePackIDs: nonNil(baseIDs), SelectedCapabilities: nonNil(capIDs), IncludeStarterFiles: req.IncludeStarter,
		Components: []model.ComponentPlan{}}
	// Harness-neutral files (same source and destination for several harnesses,
	// e.g. starter wordlists) are installed and tracked once.
	claimed := map[string]bool{}
	for _, sp := range packs {
		pm, err := LoadPack(r.Resolve(sp.manifest))
		if err != nil {
			return nil, err
		}
		for _, h := range req.Harnesses {
			if !pm.Supports(h) {
				res.Skipped = append(res.Skipped, Skip{PackID: pm.ID, Harness: h, Reason: fmt.Sprintf("pack declares no %s target (supports: %s)", h, strings.Join(pm.SupportedHarnesses(), ", "))})
				continue
			}
			plan, err := pm.Plan(h, profile.ID, sp.fallbackType, sp.capability)
			if err != nil {
				return nil, err
			}
			kept := plan.Files[:0]
			for _, f := range plan.Files {
				key := filepath.Join(pm.Dir, f.Source) + "|" + f.Destination + "|" + f.Render + "|" + f.Header
				if (f.InstallMode == "" || f.InstallMode == "copy") && claimed[key] {
					continue
				}
				claimed[key] = true
				kept = append(kept, f)
			}
			if len(kept) == 0 {
				continue
			}
			plan.Files = kept
			res.Components = append(res.Components, plan)
		}
	}
	sort.SliceStable(res.Components, func(i, j int) bool {
		a, b := res.Components[i], res.Components[j]
		if a.Harness != b.Harness {
			return harnessOrder(a.Harness) < harnessOrder(b.Harness)
		}
		return a.ID < b.ID
	})
	seen := map[string]bool{}
	for _, c := range res.Components {
		if seen[c.Key()] {
			return nil, fmt.Errorf("pack id %s is selected twice for %s; pack ids must be unique", c.ID, c.Harness)
		}
		seen[c.Key()] = true
	}
	if err := CheckDestinationCollisions(res.Components); err != nil {
		return nil, err
	}
	return res, nil
}

func harnessOrder(h string) int { return slices.Index(harness.IDs(), h) }

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ValidateCapabilitySelection enforces each group's selectionMode.
func ValidateCapabilitySelection(profile *Profile, ids []string) error {
	known := map[string]bool{}
	for _, g := range profile.CapabilityPackGroups {
		var picked []string
		for _, p := range g.Packs {
			known[p.ID] = true
			if slices.Contains(ids, p.ID) {
				picked = append(picked, p.ID)
			}
		}
		switch g.SelectionMode {
		case "zero-or-one":
			if len(picked) > 1 {
				return fmt.Errorf("capability group %s allows at most one pack; got %s", g.ID, strings.Join(picked, ", "))
			}
		case "single", "exactly-one":
			if len(picked) != 1 {
				return fmt.Errorf("capability group %s requires exactly one pack; got %d", g.ID, len(picked))
			}
		}
	}
	var unknown []string
	for _, id := range ids {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("profile %s does not define capability ids: %s", profile.ID, strings.Join(unknown, ", "))
	}
	return nil
}

// CheckDestinationCollisions rejects two components copying to one path.
// Structured merges into a shared config file are allowed.
func CheckDestinationCollisions(components []model.ComponentPlan) error {
	owner := map[string]string{}
	for _, c := range components {
		for _, f := range c.Files {
			if f.InstallMode != "" && f.InstallMode != "copy" {
				continue
			}
			key := c.Harness + "|" + f.Destination
			if prev, ok := owner[key]; ok && prev != c.ID {
				return fmt.Errorf("packs %s and %s both install %s; select only one of them", prev, c.ID, f.Destination)
			}
			owner[key] = c.ID
		}
	}
	return nil
}

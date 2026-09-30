// Package engine implements the workspace manifest engine (plan, inspect,
// audit, install, write).
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/hashx"
	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
)

const (
	ManifestRel   = ".ocws/manifest.json"
	SchemaVersion = 3
)

type Engine struct {
	Workspace     string
	TemplatesRoot string
	Version       string
	Now           func() time.Time
}

func New(workspace, templatesRoot, version string) (*Engine, error) {
	ws, err := filepath.Abs(ExpandHome(workspace))
	if err != nil {
		return nil, err
	}
	e := &Engine{Workspace: ws, Version: version, Now: time.Now}
	if templatesRoot != "" {
		e.TemplatesRoot, _ = filepath.Abs(ExpandHome(templatesRoot))
	}
	return e, nil
}

func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	return p
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ---------- manifest read / validate ----------

type ManifestStatus string

const (
	StatusMissing ManifestStatus = "missing"
	StatusInvalid ManifestStatus = "invalid"
	StatusOK      ManifestStatus = "ok"
)

type ManifestResult struct {
	Status   ManifestStatus
	Path     string
	Error    string
	Manifest *model.Manifest
}

func (e *Engine) ManifestPath() string { return filepath.Join(e.Workspace, ManifestRel) }

// ReadManifest reads .ocws/manifest.json.
func (e *Engine) ReadManifest() ManifestResult {
	path := e.ManifestPath()
	if !exists(path) {
		return ManifestResult{Status: StatusMissing, Path: path}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ManifestResult{Status: StatusInvalid, Path: path, Error: err.Error()}
	}
	m, err := ValidateManifest(data)
	if err != nil {
		return ManifestResult{Status: StatusInvalid, Path: path, Error: err.Error()}
	}
	return ManifestResult{Status: StatusOK, Path: path, Manifest: m}
}

func ValidateManifest(data []byte) (*model.Manifest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("workspace setup manifest must be a JSON object")
	}
	var m model.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("workspace setup manifest is malformed: %w", err)
	}
	if m.SchemaVersion < 1 {
		return nil, errors.New("workspace setup manifest is missing positive integer schemaVersion")
	}
	if _, ok := raw["generatedBy"]; !ok {
		return nil, errors.New("workspace setup manifest is missing generatedBy")
	}
	if _, ok := raw["generatedAt"]; !ok {
		return nil, errors.New("workspace setup manifest is missing generatedAt")
	}
	if _, ok := raw["components"]; !ok {
		return nil, errors.New("workspace setup manifest is missing components array")
	}
	// Managed defaults to true when absent; encoding/json gives false.
	var rawComps struct {
		Components []struct {
			Files []map[string]json.RawMessage `json:"files"`
		} `json:"components"`
	}
	json.Unmarshal(data, &rawComps)
	for i := range m.Components {
		c := &m.Components[i]
		if !model.ValidComponentType(c.ComponentType) {
			return nil, fmt.Errorf("manifest component at index %d has invalid componentType", i)
		}
		if strings.TrimSpace(c.ID) == "" {
			return nil, fmt.Errorf("manifest component at index %d is missing id", i)
		}
		if c.Files == nil {
			return nil, fmt.Errorf("manifest component %s is missing files array", c.ID)
		}
		if c.Capability != nil && c.Capability.SelectedBy != "" && !contains(model.SelectedByValues, c.Capability.SelectedBy) {
			return nil, fmt.Errorf("manifest component %s capability.selectedBy must be one of %s", c.ID, strings.Join(model.SelectedByValues, ", "))
		}
		for j := range c.Files {
			f := &c.Files[j]
			if f.Source == "" || f.Destination == "" || f.SourceSha256 == "" || f.InstalledSha256 == "" {
				return nil, fmt.Errorf("manifest file at %s[%d] is missing source/destination/sha256 fields", c.ID, j)
			}
			f.Destination = filepath.ToSlash(f.Destination)
			if i < len(rawComps.Components) && j < len(rawComps.Components[i].Files) {
				if _, ok := rawComps.Components[i].Files[j]["managed"]; !ok {
					f.Managed = true
				}
			}
		}
	}
	return &m, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func indexComponents(m *model.Manifest) map[string]*model.ComponentRecord {
	out := map[string]*model.ComponentRecord{}
	if m == nil {
		return out
	}
	for i := range m.Components {
		out[m.Components[i].Key()] = &m.Components[i]
	}
	return out
}

func indexFiles(c *model.ComponentRecord) map[string]*model.FileRecord {
	out := map[string]*model.FileRecord{}
	if c == nil {
		return out
	}
	for i := range c.Files {
		out[c.Files[i].Destination] = &c.Files[i]
	}
	return out
}

// ---------- path resolution ----------

func (e *Engine) candidates(value string) []string {
	v := ExpandHome(value)
	if filepath.IsAbs(v) {
		return []string{filepath.Clean(v)}
	}
	var out []string
	seen := map[string]bool{}
	for _, base := range []string{e.TemplatesRoot, e.Workspace} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, v)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func (e *Engine) resolveExisting(value string) string {
	c := e.candidates(value)
	for _, p := range c {
		if exists(p) {
			return p
		}
	}
	if len(c) == 0 {
		return ""
	}
	return c[0]
}

func (e *Engine) sourceBase(root, manifest string) string {
	if root != "" {
		return e.resolveExisting(root)
	}
	if manifest != "" {
		return filepath.Dir(e.resolveExisting(manifest))
	}
	return ""
}

func (e *Engine) resolveSource(root, manifest, value string) string {
	v := ExpandHome(value)
	if filepath.IsAbs(v) {
		return filepath.Clean(v)
	}
	base := e.sourceBase(root, manifest)
	if base == "" {
		return ""
	}
	return filepath.Join(base, v)
}

func (e *Engine) resolveDest(dest string) string {
	d := ExpandHome(dest)
	if filepath.IsAbs(d) {
		return filepath.Clean(d)
	}
	return filepath.Join(e.Workspace, d)
}

func (e *Engine) workspaceRel(p string) string {
	rel, err := filepath.Rel(e.Workspace, p)
	if err != nil || rel == "" {
		return "."
	}
	return filepath.ToSlash(rel)
}

func (e *Engine) withinWorkspace(p string) bool {
	rel, err := filepath.Rel(e.Workspace, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// templateRel stores template paths relative to the templates root so
// manifests survive moving machines.
func (e *Engine) templateRel(p string) string {
	if p == "" || e.TemplatesRoot == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(e.TemplatesRoot, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.ToSlash(rel)
}

// ---------- sources ----------

// source is the resolved content a destination should match: a path for copy
// mode or rendered bytes for render mode.
type source struct {
	path     string
	exists   bool
	sha      string
	rendered []byte
	err      error
}

func (e *Engine) loadSource(root, manifest string, f model.FilePlan) source {
	s := source{path: e.resolveSource(root, manifest, f.Source)}
	if s.path == "" {
		return s
	}
	if f.Render == "" {
		if !exists(s.path) {
			return s
		}
		s.exists = true
		s.sha, s.err = hashx.Path(s.path)
		return s
	}
	body, err := os.ReadFile(s.path)
	if err != nil {
		return s
	}
	var header []byte
	if f.Header != "" {
		if header, err = os.ReadFile(e.resolveSource(root, manifest, f.Header)); err != nil {
			return s
		}
	}
	s.exists = true
	s.rendered, s.err = harness.RenderFile(f.Render, body, header, f.Destination)
	if s.err == nil {
		s.sha = hashx.Bytes(s.rendered)
	}
	return s
}

// content returns the bytes a structured install merges: the rendered output
// for render modes, otherwise the source file.
func (s source) content() []byte {
	if s.rendered != nil {
		return s.rendered
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	return data
}

func structured(installMode string) bool {
	m := strings.ToLower(strings.TrimSpace(installMode))
	return m != "" && m != "copy"
}

func hashIfExists(p string) (string, bool) {
	if !exists(p) {
		return "", false
	}
	sum, err := hashx.Path(p)
	if err != nil {
		return "", false
	}
	return sum, true
}

// ---------- version compare ----------

var dotted = regexp.MustCompile(`^\d+(\.\d+)*$`)

func CompareVersions(installed, source string) string {
	a, b := strings.TrimSpace(installed), strings.TrimSpace(source)
	if a == "" || b == "" {
		return "unknown"
	}
	a = strings.TrimPrefix(strings.TrimPrefix(a, "v"), "V")
	b = strings.TrimPrefix(strings.TrimPrefix(b, "v"), "V")
	if a == b {
		return "same"
	}
	if !dotted.MatchString(a) || !dotted.MatchString(b) {
		return "different"
	}
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(ap), len(bp)); i++ {
		var x, y int
		if i < len(ap) {
			x, _ = strconv.Atoi(ap[i])
		}
		if i < len(bp) {
			y, _ = strconv.Atoi(bp[i])
		}
		if y > x {
			return "source-newer"
		}
		if y < x {
			return "installed-newer"
		}
	}
	return "same"
}

func sortRecords(list []model.ComponentRecord) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Key() < list[j].Key() })
}

func writeManifest(path string, m *model.Manifest) error {
	return jsonx.WriteJSONAtomic(path, jsonx.FromGo(m))
}

package engine

import (
	"encoding/json"
	"os"

	"github.com/c0dn/ocws/internal/hashx"
	"github.com/c0dn/ocws/internal/model"
)

type InspectComponent struct {
	Harness              string            `json:"harness"`
	ID                   string            `json:"id"`
	ComponentType        string            `json:"componentType"`
	ProfileID            string            `json:"profileId,omitempty"`
	Capability           *model.Capability `json:"capability,omitempty"`
	InstalledVersion     string            `json:"installedVersion,omitempty"`
	CurrentSourceVersion string            `json:"currentSourceVersion,omitempty"`
	VersionRelation      string            `json:"versionRelation"`
	SourceManifestPath   string            `json:"sourceManifestPath,omitempty"`
	SourceManifestStatus string            `json:"sourceManifestStatus"`
	RefreshState         string            `json:"refreshState"`
	RefreshRecommended   bool              `json:"refreshRecommended"`
	FileSummary          map[string]int    `json:"fileSummary"`
}

type InspectResult struct {
	ManifestPath          string             `json:"manifestPath"`
	ManifestStatus        ManifestStatus     `json:"manifestStatus"`
	ManifestError         string             `json:"manifestError,omitempty"`
	ManifestSchemaVersion int                `json:"manifestSchemaVersion,omitempty"`
	ProjectType           string             `json:"projectType,omitempty"`
	Harnesses             []string           `json:"harnesses,omitempty"`
	Components            []InspectComponent `json:"components"`
}

type sourceManifestMeta struct {
	path          string
	exists        bool
	sha           string
	version       string
	schemaVersion int
}

func readSourceManifestMeta(path string) sourceManifestMeta {
	m := sourceManifestMeta{path: path}
	if path == "" || !exists(path) {
		return m
	}
	m.exists = true
	m.sha, _ = hashx.File(path)
	if data, err := os.ReadFile(path); err == nil {
		var parsed struct {
			Version       any `json:"version"`
			SchemaVersion any `json:"schemaVersion"`
		}
		if json.Unmarshal(data, &parsed) == nil {
			if v, ok := parsed.Version.(string); ok {
				m.version = v
			}
			if v, ok := parsed.SchemaVersion.(float64); ok && v == float64(int(v)) {
				m.schemaVersion = int(v)
			}
		}
	}
	return m
}

// Inspect reports the refresh state of every installed component.
func (e *Engine) Inspect(filter []string) *InspectResult {
	mr := e.ReadManifest()
	res := &InspectResult{ManifestPath: mr.Path, ManifestStatus: mr.Status, ManifestError: mr.Error, Components: []InspectComponent{}}
	if mr.Status != StatusOK {
		return res
	}
	m := mr.Manifest
	res.ManifestSchemaVersion, res.ProjectType, res.Harnesses = m.SchemaVersion, m.ProjectType, m.Harnesses
	for _, c := range m.Components {
		if len(filter) > 0 && !contains(filter, c.ID) && !contains(filter, c.Key()) {
			continue
		}
		res.Components = append(res.Components, e.inspectComponent(c))
	}
	return res
}

func (e *Engine) inspectComponent(c model.ComponentRecord) InspectComponent {
	var smPath string
	if c.SourceManifest != "" {
		smPath = e.resolveExisting(c.SourceManifest)
	}
	sm := readSourceManifestMeta(smPath)
	fs := map[string]int{"current": 0, "source-updated-local-unchanged": 0, "source-updated-local-modified": 0,
		"locally-modified": 0, "missing-source": 0, "missing-destination": 0, "source-path-unresolved": 0}
	for _, f := range c.Files {
		src := e.loadSource(c.SourceRoot, c.SourceManifest, model.FilePlan{Source: f.Source, Render: f.Render, Header: f.Header})
		destSha, destExists := hashIfExists(e.resolveDest(f.Destination))
		var state string
		switch {
		case src.path == "":
			state = "source-path-unresolved"
		case !src.exists:
			state = "missing-source"
		case !destExists:
			state = "missing-destination"
		case src.sha != f.SourceSha256 && destSha != f.InstalledSha256:
			state = "source-updated-local-modified"
		case src.sha != f.SourceSha256:
			state = "source-updated-local-unchanged"
		case destSha != f.InstalledSha256:
			state = "locally-modified"
		default:
			state = "current"
		}
		fs[state]++
	}
	smStatus := "unknown"
	switch {
	case c.SourceManifest == "":
		smStatus = "untracked"
	case !sm.exists:
		smStatus = "missing"
	case c.SourceManifestSha256 != "" && c.SourceManifestSha256 == sm.sha:
		smStatus = "same"
	case c.SourceManifestSha256 != "":
		smStatus = "changed"
	}
	rel := CompareVersions(c.Version, sm.version)
	var state string
	switch {
	case c.SourceManifest == "" && len(c.Files) == 0:
		state = "untracked-source"
	case smStatus == "missing" || fs["missing-source"] > 0 || fs["source-path-unresolved"] > 0:
		state = "source-missing"
	case fs["missing-destination"] > 0:
		state = "incomplete-install"
	case fs["source-updated-local-modified"] > 0:
		state = "refresh-with-local-conflicts"
	case smStatus == "changed" || rel == "source-newer" || fs["source-updated-local-unchanged"] > 0:
		state = "refresh-available"
	case fs["locally-modified"] > 0:
		state = "locally-modified"
	default:
		state = "current"
	}
	return InspectComponent{Harness: c.HarnessID(), ID: c.ID, ComponentType: c.ComponentType, ProfileID: c.ProfileID, Capability: c.Capability,
		InstalledVersion: c.Version, CurrentSourceVersion: sm.version, VersionRelation: rel, SourceManifestPath: sm.path,
		SourceManifestStatus: smStatus, RefreshState: state, FileSummary: fs,
		RefreshRecommended: state == "refresh-available" || state == "refresh-with-local-conflicts" || state == "incomplete-install"}
}

package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/c0dn/ocws/internal/hashx"
	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/model"
)

type WriteOptions struct {
	ProjectType    string
	Harnesses      []string
	RemoveKeys     []string
	RebuildInvalid bool
}

type WriteResult struct {
	ManifestPath   string   `json:"manifestPath"`
	Written        []string `json:"writtenComponents"`
	Removed        []string `json:"removedComponents"`
	ComponentCount int      `json:"componentCount"`
	FileCount      int      `json:"fileCount"`
}

func (e *Engine) generatedBy() string {
	if e.Version == "" {
		return "ocws"
	}
	return "ocws " + e.Version
}

func (e *Engine) now() string {
	if e.Now == nil {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return e.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (e *Engine) buildRecord(c model.ComponentPlan) (model.ComponentRecord, error) {
	rec := model.ComponentRecord{Harness: c.Harness, ComponentType: c.ComponentType, ID: c.ID,
		SourceRoot: e.templateRel(c.SourceRoot), SourceManifest: e.templateRel(c.SourceManifest),
		ManifestSchemaVersion: c.ManifestSchemaVersion, ProfileID: c.ProfileID, Capability: c.Capability, Version: c.Version}
	for _, f := range c.Files {
		src := e.loadSource(c.SourceRoot, c.SourceManifest, f)
		if src.path == "" {
			return rec, fmt.Errorf("component %s cannot resolve source path for %s", c.ID, f.Source)
		}
		if !src.exists {
			return rec, fmt.Errorf("component %s source path not found: %s", c.ID, src.path)
		}
		if src.err != nil {
			return rec, fmt.Errorf("component %s: %w", c.ID, src.err)
		}
		destPath := e.resolveDest(f.Destination)
		destSha, ok := hashIfExists(destPath)
		if !ok {
			return rec, fmt.Errorf("component %s destination path not found: %s", c.ID, destPath)
		}
		fr := model.FileRecord{Source: filepath.ToSlash(f.Source), Destination: e.workspaceRel(destPath), SourceSha256: src.sha,
			InstalledSha256: destSha, Managed: f.IsManaged(), Role: f.Role, InstallMode: f.InstallMode, JSONPointers: f.JSONPointers,
			Render: f.Render, Header: f.Header}
		if f.InstallMode == "merge" {
			frag := src.content()
			cur, derr := os.ReadFile(destPath)
			if frag != nil && derr == nil {
				fr.PointerSha256 = jsonx.PointerHashes(frag, cur, f.JSONPointers)
			}
		}
		if !structured(f.InstallMode) && fr.SourceSha256 != fr.InstalledSha256 {
			return rec, fmt.Errorf("component %s destination %s differs from its source; install it successfully before recording it", c.ID, fr.Destination)
		}
		rec.Files = append(rec.Files, fr)
	}
	sortFiles(rec.Files)
	if c.SourceManifest != "" {
		if p := e.resolveExisting(c.SourceManifest); exists(p) {
			rec.SourceManifestSha256, _ = hashx.File(p)
			if rec.Version == "" {
				rec.Version = readSourceManifestMeta(p).version
			}
		}
	}
	return rec, nil
}

func sortFiles(files []model.FileRecord) {
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && hashx.LocaleCompare(files[j].Destination, files[j-1].Destination) < 0; j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

// Write records installed components in .ocws/manifest.json.
func (e *Engine) Write(components []model.ComponentPlan, opts WriteOptions) (*WriteResult, error) {
	mr := e.ReadManifest()
	if mr.Status == StatusInvalid && !opts.RebuildInvalid {
		return nil, fmt.Errorf("existing workspace setup manifest is invalid: %s. Re-run with --rebuild-invalid-manifest to replace it", mr.Error)
	}
	base := &model.Manifest{SchemaVersion: SchemaVersion, Components: nil}
	if mr.Status == StatusOK {
		base = mr.Manifest
	}
	byKey := map[string]model.ComponentRecord{}
	for _, c := range base.Components {
		c.Harness = c.HarnessID()
		byKey[c.Key()] = c
	}
	res := &WriteResult{ManifestPath: e.ManifestPath(), Written: []string{}, Removed: []string{}}
	for _, c := range components {
		rec, err := e.buildRecord(c)
		if err != nil {
			return nil, err
		}
		byKey[c.Key()] = rec
		res.Written = append(res.Written, c.Key())
	}
	for _, k := range opts.RemoveKeys {
		if _, ok := byKey[k]; ok {
			delete(byKey, k)
			res.Removed = append(res.Removed, k)
		}
	}
	next := &model.Manifest{SchemaVersion: SchemaVersion, GeneratedBy: e.generatedBy(), GeneratedAt: e.now(),
		ProjectType: base.ProjectType, Harnesses: mergeHarnesses(base.Harnesses, opts.Harnesses)}
	if opts.ProjectType != "" {
		next.ProjectType = opts.ProjectType
	}
	for _, c := range byKey {
		next.Components = append(next.Components, c)
		res.FileCount += len(c.Files)
	}
	if next.Components == nil {
		next.Components = []model.ComponentRecord{}
	}
	sortRecords(next.Components)
	res.ComponentCount = len(next.Components)
	if err := writeManifest(e.ManifestPath(), next); err != nil {
		return nil, err
	}
	return res, nil
}

func mergeHarnesses(a, b []string) []string {
	out := append([]string{}, a...)
	for _, h := range b {
		if !contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

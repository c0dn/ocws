package engine

import (
	"fmt"

	"github.com/c0dn/ocws/internal/model"
)

type AuditFile struct {
	Source                  string `json:"source"`
	Destination             string `json:"destination"`
	SourceExists            bool   `json:"sourceExists"`
	DestinationExists       bool   `json:"destinationExists"`
	SourceSha256            string `json:"sourceSha256,omitempty"`
	DestinationSha256       string `json:"destinationSha256,omitempty"`
	PreviousInstalledSha256 string `json:"previousInstalledSha256,omitempty"`
	State                   string `json:"state"`
	Managed                 bool   `json:"managed"`
	InstallMode             string `json:"installMode,omitempty"`
	Note                    string `json:"note,omitempty"`
}

type StaleFile struct {
	Destination             string `json:"destination"`
	PreviousInstalledSha256 string `json:"previousInstalledSha256"`
	Reason                  string `json:"reason"`
}

type AuditComponent struct {
	Harness       string            `json:"harness"`
	ComponentType string            `json:"componentType"`
	ID            string            `json:"id"`
	ProfileID     string            `json:"profileId,omitempty"`
	Capability    *model.Capability `json:"capability,omitempty"`
	Files         []AuditFile       `json:"files"`
	StaleFiles    []StaleFile       `json:"staleFiles"`
}

type StaleComponent struct {
	Harness string   `json:"harness"`
	ID      string   `json:"id"`
	Files   []string `json:"files"`
	Reason  string   `json:"reason"`
}

type AuditResult struct {
	ManifestPath    string           `json:"manifestPath"`
	ManifestStatus  ManifestStatus   `json:"manifestStatus"`
	ManifestError   string           `json:"manifestError,omitempty"`
	Summary         map[string]int   `json:"summary"`
	Components      []AuditComponent `json:"components"`
	StaleComponents []StaleComponent `json:"staleComponents"`
}

// Conflicts lists destinations that a safe-refresh install would block on.
func (a *AuditResult) Conflicts() []AuditFile {
	var out []AuditFile
	for _, c := range a.Components {
		for _, f := range c.Files {
			switch f.State {
			case "locally-modified", "outdated-locally-modified":
				out = append(out, f)
			case "untracked":
				if f.DestinationExists && f.SourceSha256 != f.DestinationSha256 && !structured(f.InstallMode) {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// Audit compares a plan against the manifest and the files on disk.
func (e *Engine) Audit(components []model.ComponentPlan) *AuditResult {
	mr := e.ReadManifest()
	existing := indexComponents(mr.Manifest)
	requested := map[string]bool{}
	res := &AuditResult{ManifestPath: mr.Path, ManifestStatus: mr.Status, ManifestError: mr.Error,
		Summary: map[string]int{"up-to-date": 0, "outdated-unmodified": 0, "locally-modified": 0,
			"outdated-locally-modified": 0, "stale": 0, "untracked": 0, "stale_components": 0}}

	for _, comp := range components {
		requested[comp.Key()] = true
		prevComp := existing[comp.Key()]
		prevFiles := indexFiles(prevComp)
		planned := map[string]bool{}
		out := AuditComponent{Harness: comp.Harness, ComponentType: comp.ComponentType, ID: comp.ID, ProfileID: comp.ProfileID, Capability: comp.Capability, StaleFiles: []StaleFile{}}
		if out.ProfileID == "" && prevComp != nil {
			out.ProfileID = prevComp.ProfileID
		}
		for _, f := range comp.Files {
			destPath := e.resolveDest(f.Destination)
			destRel := e.workspaceRel(destPath)
			planned[destRel] = true
			src := e.loadSource(comp.SourceRoot, comp.SourceManifest, f)
			destSha, destExists := hashIfExists(destPath)
			prev := prevFiles[destRel]
			var prevInstalled, prevSource string
			if prev != nil {
				prevInstalled, prevSource = prev.InstalledSha256, prev.SourceSha256
			}
			mode := f.InstallMode
			if mode == "" && prev != nil {
				mode = prev.InstallMode
			}
			isStructured := structured(mode)
			af := AuditFile{Source: f.Source, Destination: destRel, SourceExists: src.exists, DestinationExists: destExists,
				SourceSha256: src.sha, DestinationSha256: destSha, PreviousInstalledSha256: prevInstalled, Managed: f.IsManaged(), InstallMode: mode, State: "untracked"}
			if src.path == "" {
				af.Note = fmt.Sprintf("Component %s does not define sourceRoot or sourceManifest for relative source %s", comp.ID, f.Source)
			}
			if src.err != nil {
				af.Note = src.err.Error()
			}
			switch {
			case mr.Status != StatusOK || prev == nil:
				af.State = "untracked"
			case !src.exists:
				af.State = "stale"
				if af.Note == "" {
					af.Note = "Source path no longer exists: " + src.path
				}
			case isStructured && prevInstalled != "" && prevSource != "" && destSha == prevInstalled && src.sha == prevSource:
				af.State = "up-to-date"
			case destSha != "" && src.sha != "" && destSha == src.sha && !isStructured:
				af.State = "up-to-date"
			case prevInstalled != "" && destSha == prevInstalled && ((isStructured && src.sha != prevSource) || (!isStructured && src.sha != prevInstalled)):
				af.State = "outdated-unmodified"
			case prevInstalled != "" && destSha != prevInstalled && ((isStructured && src.sha == prevSource) || (!isStructured && src.sha == prevInstalled)):
				af.State = "locally-modified"
			case prevInstalled != "":
				af.State = "outdated-locally-modified"
			}
			if isStructured && af.Note == "" {
				af.Note = fmt.Sprintf("Audit used %s install mode history instead of direct source-vs-destination hash comparison.", mode)
			}
			res.Summary[af.State]++
			out.Files = append(out.Files, af)
		}
		if prevComp != nil {
			for _, pf := range prevComp.Files {
				if !planned[pf.Destination] {
					out.StaleFiles = append(out.StaleFiles, StaleFile{pf.Destination, pf.InstalledSha256, "Previously managed destination is not part of the current component plan"})
				}
			}
		}
		res.Summary["stale"] += len(out.StaleFiles)
		res.Components = append(res.Components, out)
	}
	res.StaleComponents = []StaleComponent{}
	if mr.Manifest != nil {
		for _, c := range mr.Manifest.Components {
			if requested[c.Key()] {
				continue
			}
			sc := StaleComponent{Harness: c.HarnessID(), ID: c.ID, Reason: "Previously managed component is not part of the current setup plan"}
			for _, f := range c.Files {
				sc.Files = append(sc.Files, f.Destination)
			}
			res.StaleComponents = append(res.StaleComponents, sc)
		}
	}
	res.Summary["stale_components"] = len(res.StaleComponents)
	return res
}

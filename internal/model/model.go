// Package model holds the component-plan and workspace-manifest types shared
// by the registry, engine, and CLI.
package model

import "slices"

var ComponentTypes = []string{"command-pack", "agent-pack", "skill-pack", "tool-pack", "template-pack", "config-template"}

var SelectedByValues = []string{"default", "detected", "user", "dependency", "legacy"}

func ValidComponentType(t string) bool { return slices.Contains(ComponentTypes, t) }

type Capability struct {
	ID              string `json:"id,omitempty"`
	GroupID         string `json:"groupId,omitempty"`
	DisplayName     string `json:"displayName,omitempty"`
	Optional        *bool  `json:"optional,omitempty"`
	SelectedBy      string `json:"selectedBy,omitempty"`
	SelectionReason string `json:"selectionReason,omitempty"`
	InstallMode     string `json:"installMode,omitempty"`
}

func (c *Capability) Empty() bool {
	return c == nil || *c == Capability{}
}

// MergeCapability overlays non-empty incoming fields on existing.
func MergeCapability(existing, incoming *Capability) *Capability {
	if existing.Empty() && incoming.Empty() {
		return nil
	}
	out := Capability{}
	if existing != nil {
		out = *existing
	}
	if incoming != nil {
		if incoming.ID != "" {
			out.ID = incoming.ID
		}
		if incoming.GroupID != "" {
			out.GroupID = incoming.GroupID
		}
		if incoming.DisplayName != "" {
			out.DisplayName = incoming.DisplayName
		}
		if incoming.Optional != nil {
			out.Optional = incoming.Optional
		}
		if incoming.SelectedBy != "" {
			out.SelectedBy = incoming.SelectedBy
		}
		if incoming.SelectionReason != "" {
			out.SelectionReason = incoming.SelectionReason
		}
		if incoming.InstallMode != "" {
			out.InstallMode = incoming.InstallMode
		}
	}
	return &out
}

type FilePlan struct {
	Source       string   `json:"source"`
	Destination  string   `json:"destination"`
	Managed      *bool    `json:"managed,omitempty"`
	Role         string   `json:"role,omitempty"`
	InstallMode  string   `json:"installMode,omitempty"`
	JSONPointers []string `json:"jsonPointers,omitempty"`
	// Render builds the destination from Source (body) plus Header.
	Render string `json:"render,omitempty"`
	Header string `json:"header,omitempty"`
}

func (f FilePlan) IsManaged() bool { return f.Managed == nil || *f.Managed }

type ComponentPlan struct {
	Harness               string      `json:"harness"`
	ComponentType         string      `json:"componentType"`
	ID                    string      `json:"id"`
	DisplayName           string      `json:"displayName,omitempty"`
	SourceRoot            string      `json:"sourceRoot,omitempty"`
	SourceManifest        string      `json:"sourceManifest,omitempty"`
	Version               string      `json:"version,omitempty"`
	ManifestSchemaVersion int         `json:"manifestSchemaVersion,omitempty"`
	ProfileID             string      `json:"profileId,omitempty"`
	Capability            *Capability `json:"capability,omitempty"`
	Files                 []FilePlan  `json:"files"`
}

func (c ComponentPlan) Key() string { return Key(c.Harness, c.ID) }

func Key(harness, id string) string {
	if harness == "" {
		harness = "opencode"
	}
	return harness + ":" + id
}

type FileRecord struct {
	Source          string   `json:"source"`
	Destination     string   `json:"destination"`
	SourceSha256    string   `json:"sourceSha256"`
	InstalledSha256 string   `json:"installedSha256"`
	Managed         bool     `json:"managed"`
	Role            string   `json:"role,omitempty"`
	InstallMode     string   `json:"installMode,omitempty"`
	JSONPointers    []string `json:"jsonPointers,omitempty"`
	Render          string   `json:"render,omitempty"`
	Header          string   `json:"header,omitempty"`
}

type ComponentRecord struct {
	Harness               string       `json:"harness,omitempty"`
	ComponentType         string       `json:"componentType"`
	ID                    string       `json:"id"`
	SourceRoot            string       `json:"sourceRoot,omitempty"`
	SourceManifest        string       `json:"sourceManifest,omitempty"`
	SourceManifestSha256  string       `json:"sourceManifestSha256,omitempty"`
	Version               string       `json:"version,omitempty"`
	ManifestSchemaVersion int          `json:"manifestSchemaVersion,omitempty"`
	ProfileID             string       `json:"profileId,omitempty"`
	Capability            *Capability  `json:"capability,omitempty"`
	Files                 []FileRecord `json:"files"`
}

func (c ComponentRecord) HarnessID() string {
	if c.Harness == "" {
		return "opencode"
	}
	return c.Harness
}

func (c ComponentRecord) Key() string { return Key(c.Harness, c.ID) }

// Plan rebuilds the component plan a record was written from.
func (c ComponentRecord) Plan() ComponentPlan {
	p := ComponentPlan{
		Harness: c.HarnessID(), ComponentType: c.ComponentType, ID: c.ID, SourceRoot: c.SourceRoot,
		SourceManifest: c.SourceManifest, Version: c.Version, ManifestSchemaVersion: c.ManifestSchemaVersion,
		ProfileID: c.ProfileID, Capability: c.Capability,
	}
	for _, f := range c.Files {
		managed := f.Managed
		p.Files = append(p.Files, FilePlan{Source: f.Source, Destination: f.Destination, Managed: &managed, Role: f.Role,
			InstallMode: f.InstallMode, JSONPointers: f.JSONPointers, Render: f.Render, Header: f.Header})
	}
	return p
}

type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	GeneratedBy   string            `json:"generatedBy"`
	GeneratedAt   string            `json:"generatedAt"`
	ProjectType   string            `json:"projectType,omitempty"`
	Harnesses     []string          `json:"harnesses,omitempty"`
	Components    []ComponentRecord `json:"components"`
}

func BoolPtr(b bool) *bool { return &b }

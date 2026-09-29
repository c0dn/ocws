// Package tui is the interactive setup wizard. It only collects choices; the
// work is done by setup.Apply, the same path the non-interactive CLI uses.
package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/c0dn/ocws/internal/detect"
	"github.com/c0dn/ocws/internal/engine"
	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/paths"
	"github.com/c0dn/ocws/internal/registry"
	"github.com/c0dn/ocws/internal/setup"
	"github.com/charmbracelet/huh"
)

// Printer renders the final report; injected by the CLI to avoid an import cycle.
var Printer func(r *setup.Report)

// DefaultHarnesses is injected by the CLI.
var DefaultHarnesses func(cfg paths.Config, eng *engine.Engine) []string

const none = "__none__"

func Run(eng *engine.Engine, reg *registry.Registry, cfg paths.Config) error {
	err := run(eng, reg, cfg)
	if errors.Is(err, huh.ErrUserAborted) {
		fmt.Println("Cancelled; nothing was changed.")
		return nil
	}
	return err
}

func run(eng *engine.Engine, reg *registry.Registry, cfg paths.Config) error {
	idx := detect.Scan(eng.Workspace)
	det := detect.Profiles(idx, reg)
	mr := eng.ReadManifest()

	// 1. Profile.
	profileID := det.Best
	if mr.Manifest != nil && mr.Manifest.ProjectType != "" {
		if _, ok := reg.Profiles[mr.Manifest.ProjectType]; ok {
			profileID = mr.Manifest.ProjectType
		}
	}
	var popts []huh.Option[string]
	for _, id := range reg.Order {
		p := reg.Profiles[id]
		label := p.Name()
		if id == det.Best {
			label += "  (detected: " + det.Reason + ")"
		}
		if mr.Manifest != nil && id == mr.Manifest.ProjectType {
			label += "  (installed)"
		}
		popts = append(popts, huh.NewOption(label, id))
	}
	if profileID == "" && len(reg.Order) > 0 {
		profileID = reg.Order[0]
	}

	// 2. Harnesses.
	hsel := DefaultHarnesses(cfg, eng)
	var hopts []huh.Option[string]
	for _, h := range harness.All {
		hopts = append(hopts, huh.NewOption(h.DisplayName, h.ID).Selected(slices.Contains(hsel, h.ID)))
	}

	pre := setup.CheckPreflight(eng.Workspace, harness.IDs())
	intro := fmt.Sprintf("Workspace: %s\nTemplates: %s", eng.Workspace, reg.Root)
	if !pre.Writable {
		return fmt.Errorf("workspace %s is not writable", eng.Workspace)
	}
	if pre.GitDirty {
		intro += "\n\n! The git tree has uncommitted changes; commit or stash first for an easy rollback."
	}
	if len(pre.ExistingPaths) > 0 {
		intro += "\n\nAlready present: " + strings.Join(pre.ExistingPaths, ", ")
	}
	if mr.Status == engine.StatusOK {
		intro += "\n\n" + inspectSummary(eng)
	}

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("ocws setup").Description(intro),
			huh.NewSelect[string]().Title("Workspace profile").Options(popts...).Value(&profileID),
			huh.NewMultiSelect[string]().Title("Harnesses").Description("Where to install agents, skills, commands and MCP config.").
				Options(hopts...).Value(&hsel).Validate(func(v []string) error {
				if len(v) == 0 {
					return errors.New("pick at least one harness")
				}
				return nil
			}),
		),
	).Run(); err != nil {
		return err
	}
	profile, _ := reg.Profile(profileID)
	hsel, _ = harness.ParseList(hsel)
	o := setup.Options{ProfileID: profileID, Harnesses: hsel, Overwrite: "safe-refresh", ConfigMode: setup.ModeMerge, AgentsMode: setup.ModeCreate, Scaffold: true}

	// 3. Packs.
	installed := map[string]bool{}
	if mr.Manifest != nil {
		for _, c := range mr.Manifest.Components {
			installed[c.ID] = true
		}
	}
	var groups []*huh.Group
	o.BasePackIDs = []string{}
	if len(profile.BasePacks) > 0 {
		var opts []huh.Option[string]
		for _, b := range profile.BasePacks {
			opts = append(opts, huh.NewOption(packLabel(reg, b.ID, b.Manifest, b.ComponentType, hsel, installed[b.ID]), b.ID).
				Selected(b.IsDefault() || installed[b.ID]))
		}
		groups = append(groups, huh.NewGroup(huh.NewMultiSelect[string]().Title("Base packs").Options(opts...).Value(&o.BasePackIDs)))
	}
	if profile.StarterFilePack != "" {
		groups = append(groups, huh.NewGroup(huh.NewConfirm().Title("Install starter files?").
			Description(packLabel(reg, "starter files", profile.StarterFilePack, "template-pack", hsel, false)).Value(&o.IncludeStarter)))
	}
	recs := detect.Capabilities(idx, profile, mr.Manifest)
	capValues := make([][]string, len(profile.CapabilityPackGroups))
	singleValues := make([]string, len(profile.CapabilityPackGroups))
	for gi, g := range profile.CapabilityPackGroups {
		defaults := g.Defaults()
		var opts []huh.Option[string]
		for _, p := range g.Packs {
			label := packLabel(reg, p.DisplayName, p.Manifest, p.ComponentType, hsel, false)
			if why, ok := recs[p.ID]; ok {
				label += "  [recommended: " + why + "]"
			}
			opts = append(opts, huh.NewOption(label, p.ID).Selected(slices.Contains(defaults, p.ID) || recs[p.ID] != ""))
		}
		title := g.DisplayName
		if title == "" {
			title = g.ID
		}
		switch g.SelectionMode {
		case "zero-or-one", "single", "exactly-one":
			singleValues[gi] = none
			for _, p := range g.Packs {
				if slices.Contains(defaults, p.ID) || recs[p.ID] != "" {
					singleValues[gi] = p.ID
					break
				}
			}
			if g.SelectionMode == "zero-or-one" {
				opts = append([]huh.Option[string]{huh.NewOption("None", none)}, opts...)
			} else if singleValues[gi] == none {
				singleValues[gi] = g.Packs[0].ID
			}
			groups = append(groups, huh.NewGroup(huh.NewSelect[string]().Title(title).Description(g.Description).Options(opts...).Value(&singleValues[gi])))
		default:
			groups = append(groups, huh.NewGroup(huh.NewMultiSelect[string]().Title(title).Description(g.Description).Options(opts...).Value(&capValues[gi])))
		}
	}
	if len(groups) > 0 {
		if err := huh.NewForm(groups...).Run(); err != nil {
			return err
		}
	}
	o.CapabilityIDs = []string{}
	for gi := range profile.CapabilityPackGroups {
		o.CapabilityIDs = append(o.CapabilityIDs, capValues[gi]...)
		if singleValues[gi] != "" && singleValues[gi] != none {
			o.CapabilityIDs = append(o.CapabilityIDs, singleValues[gi])
		}
	}

	// 4. Config + AGENTS.md.
	var fields []huh.Field
	var existingCfg []string
	for _, h := range hsel {
		if _, ok := profile.WorkspaceConfig[h]; ok {
			hs, _ := harness.Get(h)
			if _, err := os.Stat(filepath.Join(eng.Workspace, hs.Tokens["config"])); err == nil {
				existingCfg = append(existingCfg, hs.Tokens["config"])
			}
		}
	}
	if len(existingCfg) > 0 {
		fields = append(fields, huh.NewSelect[string]().Title("Existing "+strings.Join(existingCfg, ", ")).
			Description("How to apply the profile's config template.").
			Options(huh.NewOption("Merge: add missing keys, keep my values and permissions", setup.ModeMerge),
				huh.NewOption("Replace with the template", setup.ModeReplace),
				huh.NewOption("Leave untouched", setup.ModeSkip)).Value(&o.ConfigMode))
	}
	_, agentsErr := os.Stat(filepath.Join(eng.Workspace, "AGENTS.md"))
	if agentsErr == nil {
		fields = append(fields, huh.NewSelect[string]().Title("AGENTS.md already exists").
			Options(huh.NewOption("Keep it", setup.ModeCreate), huh.NewOption("Regenerate from the profile guide", setup.ModeReplace)).Value(&o.AgentsMode))
	} else {
		fields = append(fields, huh.NewSelect[string]().Title("Generate AGENTS.md?").
			Options(huh.NewOption("Yes", setup.ModeCreate), huh.NewOption("No", setup.ModeSkip)).Value(&o.AgentsMode))
	}
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return err
	}
	if o.AgentsMode == setup.ModeReplace || (o.AgentsMode == setup.ModeCreate && agentsErr != nil) {
		o.Agents.Name = filepath.Base(eng.Workspace)
		var conv string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Project name").Value(&o.Agents.Name),
			huh.NewText().Title("Short description").Value(&o.Agents.Description),
			huh.NewText().Title("Conventions worth preserving").Description("One per line; optional.").Value(&conv),
		)).Run(); err != nil {
			return err
		}
		o.Agents.Conventions = strings.Split(conv, "\n")
	}

	// 5. Plan, conflicts, confirm.
	rep, err := setup.Apply(eng, reg, withDry(o))
	if err != nil && !errors.Is(err, setup.ErrBlocked) {
		return err
	}
	conflicts := blockedFiles(rep)
	stale := 0
	for _, c := range rep.Audit.Components {
		stale += len(c.StaleFiles)
	}
	var confirmFields []huh.Field
	summary := planSummary(rep)
	confirmFields = append(confirmFields, huh.NewNote().Title("Plan").Description(summary))
	if len(conflicts) > 0 {
		o.Overwrite = "overwrite-approved"
		confirmFields = append(confirmFields, huh.NewSelect[string]().Title(fmt.Sprintf("%d file(s) differ from the templates", len(conflicts))).
			Description(strings.Join(conflicts, "\n")).
			Options(huh.NewOption("Overwrite them with the template versions", "overwrite-approved"),
				huh.NewOption("Cancel setup", "cancel")).Value(&o.Overwrite))
	}
	if stale > 0 {
		confirmFields = append(confirmFields, huh.NewConfirm().Title(fmt.Sprintf("Remove %d file(s) the new pack versions no longer ship?", stale)).
			Description("Unchanged files are deleted; locally edited ones are kept unless you chose overwrite.").Value(&o.Prune))
	}
	apply := true
	confirmFields = append(confirmFields, huh.NewConfirm().Title("Apply?").Affirmative("Apply").Negative("Cancel").Value(&apply))
	if err := huh.NewForm(huh.NewGroup(confirmFields...)).Run(); err != nil {
		return err
	}
	if !apply || o.Overwrite == "cancel" {
		fmt.Println("Cancelled; nothing was changed.")
		return nil
	}
	rep, err = setup.Apply(eng, reg, o)
	if rep != nil && Printer != nil {
		Printer(rep)
	}
	if errors.Is(err, setup.ErrBlocked) {
		return errors.New("install blocked; nothing was written")
	}
	return err
}

func withDry(o setup.Options) setup.Options { o.DryRun = true; return o }

func packLabel(reg *registry.Registry, name, manifest, ctype string, harnesses []string, installed bool) string {
	label := name
	if pm, err := registry.LoadPack(reg.Resolve(manifest)); err == nil {
		if pm.DisplayName != "" && (name == "" || name == pm.ID) {
			label = pm.DisplayName
		}
		var missing []string
		for _, h := range harnesses {
			if !pm.Supports(h) {
				missing = append(missing, h)
			}
		}
		if ctype == "" {
			ctype = pm.ComponentType
		}
		label = fmt.Sprintf("%s (%s %s)", label, ctype, pm.Version)
		if len(missing) > 0 {
			label += "  - not for " + strings.Join(missing, ", ")
		}
	}
	if installed {
		label += "  [installed]"
	}
	return label
}

func inspectSummary(eng *engine.Engine) string {
	res := eng.Inspect(nil)
	counts := map[string][]string{}
	for _, c := range res.Components {
		counts[c.RefreshState] = append(counts[c.RefreshState], c.Harness+":"+c.ID)
	}
	var lines []string
	for _, st := range []string{"refresh-available", "refresh-with-local-conflicts", "incomplete-install", "locally-modified", "source-missing", "current"} {
		if l := counts[st]; len(l) > 0 {
			lines = append(lines, fmt.Sprintf("%s: %s", st, strings.Join(l, ", ")))
		}
	}
	return "Installed components:\n" + strings.Join(lines, "\n")
}

func blockedFiles(r *setup.Report) []string {
	var out []string
	if r == nil || r.Install == nil {
		return out
	}
	for _, c := range r.Install.Components {
		for _, f := range c.Files {
			if f.Status == "blocked-conflict" || f.Status == "blocked-stale-conflict" {
				out = append(out, c.Harness+": "+f.Destination)
			}
		}
	}
	return out
}

func planSummary(r *setup.Report) string {
	var b strings.Builder
	for _, c := range r.Plan.Components {
		counts := map[string]int{}
		if r.Install != nil {
			for _, ic := range r.Install.Components {
				if ic.Harness == c.Harness && ic.ID == c.ID {
					for _, f := range ic.Files {
						counts[strings.TrimPrefix(f.Status, "dry-run-")]++
					}
				}
			}
		}
		var parts []string
		for _, k := range []string{"copy", "merge", "skipped-up-to-date", "adopted-existing", "blocked-conflict", "remove-stale"} {
			if counts[k] > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
			}
		}
		fmt.Fprintf(&b, "%-9s %-28s %s\n", c.Harness, c.ID, strings.Join(parts, ", "))
	}
	if len(r.Plan.Components) == 0 {
		b.WriteString("No packs selected.\n")
	}
	for _, a := range r.Actions {
		fmt.Fprintf(&b, "%s %s\n", a.Action, a.Path)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "! %s\n", w)
	}
	return strings.TrimRight(b.String(), "\n")
}

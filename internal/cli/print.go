package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/c0dn/ocws/internal/engine"
	"github.com/c0dn/ocws/internal/setup"
	"github.com/charmbracelet/lipgloss"
)

var (
	bold  = lipgloss.NewStyle().Bold(true)
	dim   = lipgloss.NewStyle().Faint(true)
	warnS = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errS  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	okS   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

func summarize(counts map[string]int) string {
	var keys []string
	for k, v := range counts {
		if v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

// PrintReport renders a plan/apply report for humans.
func PrintReport(w io.Writer, r *setup.Report) {
	p := r.Plan
	fmt.Fprintf(w, "%s %s (%s)   %s %s\n", bold.Render("Profile:"), p.ProfileDisplayName, p.ProfileID, bold.Render("Harnesses:"), strings.Join(p.Harnesses, ", "))
	if r.Audit != nil && r.Audit.ManifestStatus == engine.StatusOK {
		fmt.Fprintf(w, "%s %s\n", bold.Render("Manifest:"), r.Audit.ManifestPath)
	}
	fmt.Fprintln(w)
	perFile := map[string]map[string]int{}
	var blocked []engine.InstallFile
	if r.Install != nil {
		for _, c := range r.Install.Components {
			counts := map[string]int{}
			for _, f := range c.Files {
				counts[f.Status]++
				if strings.HasPrefix(f.Status, "blocked-") {
					blocked = append(blocked, f)
				}
			}
			perFile[c.Harness+":"+c.ID] = counts
		}
	}
	fmt.Fprintln(w, bold.Render("Components"))
	if len(p.Components) == 0 {
		fmt.Fprintln(w, dim.Render("  (none selected)"))
	}
	for _, c := range p.Components {
		fmt.Fprintf(w, "  %-9s %-30s %-16s %2d files  %s\n", c.Harness, c.ID, c.ComponentType, len(c.Files), dim.Render(summarize(perFile[c.Harness+":"+c.ID])))
	}
	if len(r.Actions) > 0 {
		fmt.Fprintln(w, "\n"+bold.Render("Workspace files"))
		for _, a := range r.Actions {
			note := ""
			if a.Note != "" {
				note = dim.Render("  " + a.Note)
			}
			fmt.Fprintf(w, "  %-14s %s%s\n", a.Action, a.Path, note)
		}
	}
	if r.Removed != nil {
		fmt.Fprintln(w, "\n"+bold.Render("Previously installed, not in this plan (removed)"))
		blocked = append(blocked, printRemoved(w, r.Removed)...)
	} else if r.Audit != nil && len(r.Audit.StaleComponents) > 0 {
		fmt.Fprintln(w, "\n"+bold.Render("Previously installed, not in this plan (left in place)"))
		for _, s := range r.Audit.StaleComponents {
			fmt.Fprintf(w, "  %-9s %s (%d files)\n", s.Harness, s.ID, len(s.Files))
		}
		fmt.Fprintln(w, dim.Render("  Re-run with --prune, or `ocws remove <id>`, to uninstall them."))
	}
	if len(blocked) > 0 {
		fmt.Fprintln(w, "\n"+errS.Render("Blocked"))
		for _, f := range blocked {
			fmt.Fprintf(w, "  %s  %s\n    %s\n", errS.Render(f.Status), f.Destination, dim.Render(f.Note))
		}
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, "\n"+warnS.Render("Warnings"))
		for _, m := range r.Warnings {
			fmt.Fprintf(w, "  ! %s\n", m)
		}
	}
	if len(r.Notes) > 0 {
		fmt.Fprintln(w, "\n"+bold.Render("Notes"))
		for _, m := range dedupe(r.Notes) {
			fmt.Fprintf(w, "  - %s\n", m)
		}
	}
	switch {
	case r.Write != nil:
		fmt.Fprintf(w, "\n%s %s (%d components, %d files)\n", okS.Render("Done."), r.Write.ManifestPath, r.Write.ComponentCount, r.Write.FileCount)
	case len(blocked) == 0 && r.Install != nil && r.Install.DryRun:
		fmt.Fprintln(w, "\n"+dim.Render("Dry run: nothing was written."))
	}
}

// printRemoved lists uninstalled components and returns their blocked files.
func printRemoved(w io.Writer, res *engine.InstallResult) []engine.InstallFile {
	var blocked []engine.InstallFile
	for _, c := range res.Components {
		counts := map[string]int{}
		var notes []string
		for _, f := range c.Files {
			counts[f.Status]++
			if strings.HasPrefix(f.Status, "blocked-") {
				blocked = append(blocked, f)
			} else if strings.HasPrefix(f.Status, "left-merged") {
				notes = append(notes, f.Note)
			}
		}
		fmt.Fprintf(w, "  %-9s %-30s %2d files  %s\n", c.Harness, c.ID, len(c.Files), dim.Render(summarize(counts)))
		for _, n := range notes {
			fmt.Fprintf(w, "    %s\n", warnS.Render("! "+n))
		}
	}
	return blocked
}

func dedupe(l []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range l {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func printStatus(res *engine.InspectResult) {
	switch res.ManifestStatus {
	case engine.StatusMissing:
		fmt.Println("No ocws manifest in this workspace. Run `ocws` or `ocws apply` to set it up.")
		return
	case engine.StatusInvalid:
		fmt.Printf("%s %s: %s\n", errS.Render("Invalid manifest"), res.ManifestPath, res.ManifestError)
		return
	}
	fmt.Printf("%s %s (schema %d, profile %s)\n\n", bold.Render("Manifest:"), res.ManifestPath, res.ManifestSchemaVersion, res.ProjectType)
	for _, c := range res.Components {
		st := c.RefreshState
		switch st {
		case "current":
			st = okS.Render(st)
		case "refresh-available", "incomplete-install", "locally-modified":
			st = warnS.Render(st)
		default:
			st = errS.Render(st)
		}
		ver := c.InstalledVersion
		if c.CurrentSourceVersion != "" && c.CurrentSourceVersion != c.InstalledVersion {
			ver += " -> " + c.CurrentSourceVersion
		}
		fmt.Printf("  %-9s %-30s %-14s %-28s %s\n", c.Harness, c.ID, ver, st, dim.Render(summarize(c.FileSummary)))
	}
}

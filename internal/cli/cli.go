// Package cli wires the ocws commands.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c0dn/ocws/internal/detect"
	"github.com/c0dn/ocws/internal/engine"
	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/paths"
	"github.com/c0dn/ocws/internal/registry"
	"github.com/c0dn/ocws/internal/setup"
	"github.com/c0dn/ocws/internal/templates"
	"github.com/c0dn/ocws/internal/tui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// ExitError carries a process exit code.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

type app struct {
	version   string
	home      string
	templates string
	workspace string
	json      bool
}

func (a *app) env() (home string, cfg paths.Config, root string, err error) {
	if home, err = paths.Home(a.home); err != nil {
		return
	}
	if cfg, err = paths.LoadConfig(home); err != nil {
		return
	}
	root, err = paths.TemplatesRoot(home, a.templates, cfg)
	return
}

func (a *app) load() (*registry.Registry, *engine.Engine, paths.Config, error) {
	_, cfg, root, err := a.env()
	if err != nil {
		return nil, nil, cfg, err
	}
	reg, err := registry.Load(root)
	if err != nil {
		return nil, nil, cfg, err
	}
	eng, err := engine.New(a.workspace, root, a.version)
	return reg, eng, cfg, err
}

func (a *app) emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func New(version string) *cobra.Command {
	a := &app{version: version}
	tui.Printer = func(r *setup.Report) { PrintReport(os.Stdout, r) }
	tui.DefaultHarnesses = DefaultHarnesses
	root := &cobra.Command{
		Use:   "ocws",
		Short: "Set up agent-harness workspaces (OpenCode, Claude Code, Codex) from templates",
		Long: `ocws installs workspace profiles, agents, commands, skills, tools and MCP
config from a templates repository into the current project, and tracks what
it installed in .ocws/manifest.json so later runs can refresh safely.

Run with no arguments for the interactive setup wizard.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return cmd.Help()
			}
			reg, eng, cfg, err := a.load()
			if err != nil {
				return err
			}
			return tui.Run(eng, reg, cfg)
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&a.home, "home", "", "ocws home (default $OCWS_HOME, $XDG_CONFIG_HOME/ocws, or ~/.config/ocws)")
	pf.StringVar(&a.templates, "templates", "", "templates root (default $OCWS_TEMPLATES, config.toml templates, or <home>/templates)")
	pf.StringVarP(&a.workspace, "workspace", "C", ".", "workspace directory")
	pf.BoolVar(&a.json, "json", false, "print machine-readable JSON")

	root.AddCommand(a.initCmd(), a.templatesCmd(), a.detectCmd(), a.statusCmd(), a.planCmd(false), a.planCmd(true), a.removeCmd())
	return root
}

func (a *app) removeCmd() *cobra.Command {
	var overwrite string
	var dryRun bool
	cmd := &cobra.Command{
		Use:     "remove <component>...",
		Aliases: []string{"uninstall", "rm"},
		Short:   "Uninstall components recorded in .ocws/manifest.json",
		Long: `Removes the files an installed component manages, backs its merged
fragments out of shared config (e.g. opencode.json /mcp/<name>), and drops it
from the manifest. Files with local edits block the removal unless
--overwrite overwrite-approved is given. Files still claimed by another
installed component are kept. Components are named by id or harness:id
(see ` + "`ocws status`" + `).`,
		Example: `  ocws remove web-components-config
  ocws remove opencode:web-components-config --dry-run`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, root, err := a.env()
			if err != nil {
				return err
			}
			eng, err := engine.New(a.workspace, root, a.version)
			if err != nil {
				return err
			}
			if !contains(engine.OverwritePolicies, overwrite) {
				return fmt.Errorf("--overwrite must be one of %s", strings.Join(engine.OverwritePolicies, ", "))
			}
			keys, err := eng.ResolveInstalled(args)
			if err != nil {
				return err
			}
			opts := engine.InstallOptions{OverwritePolicy: overwrite, DryRun: true}
			res, err := eng.Uninstall(keys, nil, opts)
			if err == nil && !res.Blocked() && !dryRun {
				opts.DryRun = false
				if res, err = eng.Uninstall(keys, nil, opts); err == nil && !res.Blocked() {
					_, err = eng.Write(nil, engine.WriteOptions{RemoveKeys: keys})
				}
			}
			if res != nil {
				if a.json {
					a.emit(res)
				} else {
					fmt.Println(bold.Render("Removing"))
					if blocked := printRemoved(os.Stdout, res); len(blocked) > 0 {
						fmt.Println("\n" + errS.Render("Blocked"))
						for _, f := range blocked {
							fmt.Printf("  %s  %s\n    %s\n", errS.Render(f.Status), f.Destination, dim.Render(f.Note))
						}
					}
				}
			}
			if err != nil {
				return err
			}
			if res.Blocked() {
				if !a.json {
					fmt.Println("\nBlocked: nothing was changed. Re-run with --overwrite overwrite-approved to remove edited files anyway.")
				}
				return ExitError{2}
			}
			if !a.json {
				if dryRun {
					fmt.Println("\n" + dim.Render("Dry run: nothing was written."))
				} else {
					fmt.Printf("\n%s removed %s from %s\n", okS.Render("Done."), strings.Join(keys, ", "), eng.ManifestPath())
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&overwrite, "overwrite", "safe-refresh", "safe-refresh (keep edited files: block) | overwrite-approved (remove them anyway)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed without changing anything")
	return cmd
}

// StarterTemplates is cloned by `ocws init` when --from is not given.
const StarterTemplates = "https://github.com/c0dn/ocws-template.git"

func (a *app) initCmd() *cobra.Command {
	var from string
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the ocws home and fetch templates (git clone or copy)",
		Long:  "Create the ocws home and fetch templates. Without --from it clones the\npublic starter templates (" + StarterTemplates + ").",
		Example: `  ocws init
  ocws init --from https://github.com/you/agent-templates.git
  ocws init --from ~/src/agent-templates`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, cfg, root, err := a.env()
			if err != nil {
				return err
			}
			if from == "" {
				if _, err := registry.FindRegistry(root); err == nil && !force {
					fmt.Printf("Templates already present at %s\n", root)
					return nil
				}
				from = StarterTemplates
			}
			how, err := templates.Init(engine.ExpandHome(from), root, force)
			if err != nil {
				return err
			}
			cfg.Source = from
			if err := paths.SaveConfig(home, cfg); err != nil {
				return err
			}
			reg, err := registry.Load(root)
			if err != nil {
				return fmt.Errorf("%s templates into %s, but they do not load: %w", how, root, err)
			}
			fmt.Printf("%s %s into %s (%d profiles)\nConfig: %s\n", strings.ToUpper(how[:1])+how[1:], from, root, len(reg.Order), filepath.Join(home, paths.ConfigName))
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "git URL, git checkout, or directory to fetch templates from (default: the starter templates)")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing templates root (the old one is moved to <root>.bak)")
	return cmd
}

func (a *app) templatesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "templates", Short: "Manage the templates root"}
	cmd.AddCommand(&cobra.Command{
		Use: "path", Short: "Print the ocws home and templates root",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _, root, err := a.env()
			if err != nil {
				return err
			}
			if a.json {
				a.emit(map[string]string{"home": home, "templates": root})
				return nil
			}
			fmt.Printf("home:      %s\ntemplates: %s\n", home, root)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "update", Short: "git pull --ff-only the templates root",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, root, err := a.env()
			if err != nil {
				return err
			}
			out, err := templates.Update(root)
			fmt.Println(out)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "validate", Short: "Check profiles, packs, harness targets, sources, and renders",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, root, err := a.env()
			if err != nil {
				return err
			}
			reg, err := registry.Load(root)
			if err != nil {
				return err
			}
			issues := templates.Validate(reg)
			if a.json {
				a.emit(issues)
			} else {
				for _, i := range issues {
					fmt.Printf("%-5s %s: %s\n", i.Level, i.Where, i.Message)
				}
			}
			errs := 0
			for _, i := range issues {
				if i.Level == "error" {
					errs++
				}
			}
			if !a.json {
				fmt.Printf("%d profiles, %d issues (%d errors) in %s\n", len(reg.Order), len(issues), errs, root)
			}
			if errs > 0 {
				return ExitError{1}
			}
			return nil
		},
	})

	var hs []string
	var force bool
	port := &cobra.Command{
		Use:   "port <pack-manifest.json>...",
		Short: "Generate Claude Code / Codex targets for OpenCode packs",
		Long: `Adds targets.<harness> to each pack manifest and writes header stubs under
<pack>/harness/<harness>/. Agents become Claude subagents and Codex agent TOML;
commands become skills (Claude: disable-model-invocation: true; Codex:
allow_implicit_invocation: false); skills are copied; opencode.json MCP
fragments are translated to .mcp.json / .codex/config.toml. OpenCode
permissions and custom .ts tools are never translated.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := harness.ParseList(hs)
			if err != nil {
				return err
			}
			var all []templates.PortResult
			for _, m := range args {
				res, err := templates.Port(engine.ExpandHome(m), list, force)
				if err != nil {
					return fmt.Errorf("%s: %w", m, err)
				}
				all = append(all, res...)
			}
			if a.json {
				a.emit(all)
				return nil
			}
			for _, r := range all {
				state := fmt.Sprintf("%d files", r.Files)
				if r.Skipped {
					state = "skipped"
				}
				fmt.Printf("%-28s %-8s %s\n", r.PackID, r.Harness, state)
				for _, w := range r.Warnings {
					fmt.Printf("    ! %s\n", w)
				}
			}
			return nil
		},
	}
	port.Flags().StringSliceVar(&hs, "harness", []string{"claude", "codex"}, "harnesses to generate")
	port.Flags().BoolVar(&force, "force", false, "regenerate existing targets and headers")
	cmd.AddCommand(port)
	return cmd
}

func (a *app) detectCmd() *cobra.Command {
	return &cobra.Command{
		Use: "detect", Short: "Detect the workspace profile, harnesses in use, and recommended packs",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, eng, _, err := a.load()
			if err != nil {
				return err
			}
			idx := detect.Scan(eng.Workspace)
			d := detect.Profiles(idx, reg)
			hs := detect.Harnesses(eng.Workspace)
			var recs map[string]string
			if d.Best != "" {
				p, _ := reg.Profile(d.Best)
				recs = detect.Capabilities(idx, p, eng.ReadManifest().Manifest)
			}
			if a.json {
				a.emit(map[string]any{"profile": d, "harnesses": hs, "recommendedCapabilities": recs})
				return nil
			}
			fmt.Printf("Workspace: %s\n", eng.Workspace)
			if d.Best != "" {
				fmt.Printf("Profile:   %s (%s)\n", d.Best, d.Reason)
			} else {
				fmt.Printf("Profile:   none (%s)\n", d.Reason)
			}
			for _, s := range d.Scores {
				fmt.Printf("  %-22s score %d  %s\n", s.ProfileID, s.Score, strings.Join(s.Matches, ", "))
			}
			fmt.Printf("Harnesses in use: %s\n", orNone(hs))
			for id, why := range recs {
				fmt.Printf("Recommend %s: %s\n", id, why)
			}
			return nil
		},
	}
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use: "status", Short: "Show installed components and whether they can be refreshed",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, root, err := a.env()
			if err != nil {
				return err
			}
			eng, err := engine.New(a.workspace, root, a.version)
			if err != nil {
				return err
			}
			res := eng.Inspect(args)
			if a.json {
				a.emit(res)
				return nil
			}
			printStatus(res)
			return nil
		},
	}
}

type selection struct {
	profile, overwrite, config, agents, name, description string
	harnesses, base, caps, conventions                    []string
	starter, dryRun, prune, scaffold, rebuild             bool
}

func (a *app) planCmd(apply bool) *cobra.Command {
	s := &selection{}
	use, short := "plan", "Show what apply would install (read-only)"
	if apply {
		use, short = "apply", "Install the selected profile into the workspace"
	}
	cmd := &cobra.Command{
		Use: use, Short: short,
		Example: `  ocws apply --profile webapp --harness opencode,claude --cap postgres
  ocws plan --profile webapp --cap postgres,playwright --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, eng, cfg, err := a.load()
			if err != nil {
				return err
			}
			o, err := a.options(cmd, s, reg, eng, cfg)
			if err != nil {
				return err
			}
			if !apply {
				o.DryRun = true
			}
			rep, err := setup.Apply(eng, reg, o)
			if rep != nil {
				if a.json {
					a.emit(rep)
				} else {
					PrintReport(os.Stdout, rep)
				}
			}
			if errors.Is(err, setup.ErrBlocked) {
				if !a.json {
					fmt.Println("\nBlocked: nothing was changed. Re-run with --overwrite overwrite-approved to replace these files, or resolve them by hand.")
				}
				return ExitError{2}
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringVarP(&s.profile, "profile", "p", "", "workspace profile (default: detected)")
	f.StringSliceVar(&s.harnesses, "harness", nil, "harnesses: opencode, claude, codex (default: config, then detected, then opencode)")
	f.StringSliceVar(&s.base, "base", nil, "base pack ids (default: profile defaults; 'none' for none)")
	f.StringSliceVar(&s.caps, "cap", nil, "capability pack ids (default: group defaults; 'none' for none)")
	f.BoolVar(&s.starter, "starter", false, "include the profile's starter-file pack")
	f.StringVar(&s.overwrite, "overwrite", "safe-refresh", "missing-only | safe-refresh | overwrite-approved")
	f.BoolVar(&s.prune, "prune", false, "remove files that earlier installs managed but the new plan drops")
	f.StringVar(&s.config, "config", setup.ModeMerge, "workspace config template: merge (keep your values) | replace | skip")
	f.StringVar(&s.agents, "agents", setup.ModeCreate, "AGENTS.md: create (only if missing) | replace | skip")
	f.StringVar(&s.name, "name", "", "project name for AGENTS.md (default: directory name)")
	f.StringVar(&s.description, "description", "", "short description for AGENTS.md")
	f.StringArrayVar(&s.conventions, "convention", nil, "convention line for AGENTS.md (repeatable)")
	f.BoolVar(&s.scaffold, "scaffold", true, "create the profile's missing scaffold directories/files")
	f.BoolVar(&s.rebuild, "rebuild-invalid-manifest", false, "replace an unreadable .ocws/manifest.json")
	if apply {
		f.BoolVar(&s.dryRun, "dry-run", false, "show what would change without writing")
	}
	return cmd
}

func idList(cmd *cobra.Command, flag string, v []string) []string {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	if len(v) == 1 && v[0] == "none" {
		return []string{}
	}
	return v
}

func (a *app) options(cmd *cobra.Command, s *selection, reg *registry.Registry, eng *engine.Engine, cfg paths.Config) (setup.Options, error) {
	profile := s.profile
	if profile == "" {
		d := detect.Profiles(detect.Scan(eng.Workspace), reg)
		if d.Best == "" {
			return setup.Options{}, fmt.Errorf("could not detect a profile (%s); pass --profile (%s)", d.Reason, strings.Join(reg.Order, ", "))
		}
		profile = d.Best
		if !a.json {
			fmt.Fprintf(os.Stderr, "Detected profile %s: %s\n", profile, d.Reason)
		}
	}
	if _, err := reg.Profile(profile); err != nil {
		return setup.Options{}, err
	}
	hs, err := resolveHarnesses(s.harnesses, cfg, eng)
	if err != nil {
		return setup.Options{}, err
	}
	for _, m := range []struct {
		v, flag string
		ok      []string
	}{
		{s.overwrite, "overwrite", engine.OverwritePolicies},
		{s.config, "config", []string{setup.ModeMerge, setup.ModeReplace, setup.ModeSkip}},
		{s.agents, "agents", []string{setup.ModeCreate, setup.ModeReplace, setup.ModeSkip}},
	} {
		if !contains(m.ok, m.v) {
			return setup.Options{}, fmt.Errorf("--%s must be one of %s", m.flag, strings.Join(m.ok, ", "))
		}
	}
	return setup.Options{ProfileID: profile, Harnesses: hs, BasePackIDs: idList(cmd, "base", s.base), CapabilityIDs: idList(cmd, "cap", s.caps),
		IncludeStarter: s.starter, Overwrite: s.overwrite, DryRun: s.dryRun, Prune: s.prune, ConfigMode: s.config, AgentsMode: s.agents,
		Agents: setup.AgentsInfo{Name: s.name, Description: s.description, Conventions: s.conventions}, Scaffold: s.scaffold, RebuildInvalidManifest: s.rebuild}, nil
}

// DefaultHarnesses picks config defaults, then harnesses already present in
// the workspace or manifest, then OpenCode.
func DefaultHarnesses(cfg paths.Config, eng *engine.Engine) []string {
	if len(cfg.DefaultHarnesses) > 0 {
		if hs, err := harness.ParseList(cfg.DefaultHarnesses); err == nil {
			return hs
		}
	}
	var found []string
	if mr := eng.ReadManifest(); mr.Manifest != nil {
		found = append(found, mr.Manifest.Harnesses...)
	}
	found = append(found, detect.Harnesses(eng.Workspace)...)
	if hs, err := harness.ParseList(found); err == nil && len(hs) > 0 {
		return hs
	}
	return []string{"opencode"}
}

func resolveHarnesses(flag []string, cfg paths.Config, eng *engine.Engine) ([]string, error) {
	if len(flag) > 0 {
		return harness.ParseList(flag)
	}
	return DefaultHarnesses(cfg, eng), nil
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

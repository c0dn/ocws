package setup_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c0dn/ocws/internal/engine"
	"github.com/c0dn/ocws/internal/registry"
	"github.com/c0dn/ocws/internal/setup"
	"github.com/c0dn/ocws/internal/templates"
	"github.com/pelletier/go-toml/v2"
)

// fixture copies testdata/templates so tests can mutate sources.
func fixture(t *testing.T) (tpl, ws string) {
	t.Helper()
	tpl = filepath.Join(t.TempDir(), "templates")
	if err := templates.CopyTree("../../testdata/templates", tpl); err != nil {
		t.Fatal(err)
	}
	ws = t.TempDir()
	return
}

func load(t *testing.T, tpl, ws string) (*registry.Registry, *engine.Engine) {
	t.Helper()
	reg, err := registry.Load(tpl)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ws, tpl, "test")
	if err != nil {
		t.Fatal(err)
	}
	return reg, eng
}

func opts(harnesses ...string) setup.Options {
	return setup.Options{ProfileID: "dev", Harnesses: harnesses, BasePackIDs: []string{"dev-agents", "dev-commands", "dev-skills"},
		CapabilityIDs: []string{"docs-mcp"}, IncludeStarter: true, Overwrite: "safe-refresh", Scaffold: true,
		Agents: setup.AgentsInfo{Name: "Demo", Description: "A demo."}}
}

func read(t *testing.T, ws, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func states(eng *engine.Engine) map[string]string {
	out := map[string]string{}
	for _, c := range eng.Inspect(nil).Components {
		out[c.Harness+":"+c.ID] = c.RefreshState
	}
	return out
}

func statuses(r *setup.Report) map[string]int {
	out := map[string]int{}
	for _, c := range r.Install.Components {
		for _, f := range c.Files {
			out[f.Status]++
		}
	}
	return out
}

func TestFreshInstallAllHarnesses(t *testing.T) {
	tpl, ws := fixture(t)
	os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module x\n"), 0o644)
	reg, eng := load(t, tpl, ws)
	rep, err := setup.Apply(eng, reg, opts("opencode", "claude", "codex"))
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		".opencode/agents/reviewer.md", ".claude/agents/reviewer.md", ".codex/agents/reviewer.toml",
		".opencode/commands/ship.md", ".opencode/skills/review/SKILL.md", ".claude/skills/review/NOTES.md",
		".agents/skills/review/SKILL.md", "notes/START.md", "docs/README.md", "AGENTS.md", "CLAUDE.md",
		".mcp.json", ".codex/config.toml", "opencode.json", ".ocws/manifest.json",
	} {
		if _, err := os.Stat(filepath.Join(ws, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "docs/adr")); err != nil {
		t.Error("scaffold dir missing")
	}

	claude := read(t, ws, ".claude/agents/reviewer.md")
	if !strings.HasPrefix(claude, "---\nname: reviewer\n") || strings.Contains(claude, "mode: subagent") || !strings.Contains(claude, "Review the change.") {
		t.Errorf("claude agent render wrong:\n%s", claude)
	}
	var codex map[string]any
	if err := toml.Unmarshal([]byte(read(t, ws, ".codex/agents/reviewer.toml")), &codex); err != nil {
		t.Fatal(err)
	}
	if di, _ := codex["developer_instructions"].(string); !strings.Contains(di, `C:\\tmp`) || codex["sandbox_mode"] != "read-only" {
		t.Errorf("codex agent render wrong: %#v", codex)
	}

	var oc map[string]any
	json.Unmarshal([]byte(read(t, ws, "opencode.json")), &oc)
	if oc["share"] != "disabled" || oc["mcp"].(map[string]any)["docs"] == nil {
		t.Errorf("opencode.json not created+merged: %v", oc)
	}
	var cfg map[string]any
	toml.Unmarshal([]byte(read(t, ws, ".codex/config.toml")), &cfg)
	if cfg["approval_policy"] != "on-request" || cfg["mcp_servers"] == nil {
		t.Errorf("codex config wrong: %v", cfg)
	}
	if !strings.Contains(read(t, ws, "CLAUDE.md"), "@AGENTS.md") {
		t.Error("CLAUDE.md does not import AGENTS.md")
	}
	agents := read(t, ws, "AGENTS.md")
	if !strings.HasPrefix(agents, "# Demo\n\nA demo.\n\n## Workflow") || !strings.Contains(agents, "go test ./...") {
		t.Errorf("AGENTS.md wrong:\n%s", agents)
	}

	// Commands have no claude/codex target; tool-less packs skip with a warning.
	if len(rep.Warnings) == 0 || !strings.Contains(strings.Join(rep.Warnings, "\n"), "dev-commands not installed for claude") {
		t.Errorf("expected skip warnings, got %v", rep.Warnings)
	}
	// Harness-neutral starter file is tracked once.
	count := 0
	for _, c := range rep.Plan.Components {
		for _, f := range c.Files {
			if f.Destination == "notes/START.md" {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("starter file planned %d times", count)
	}
	for k, st := range states(eng) {
		if st != "current" {
			t.Errorf("%s: %s", k, st)
		}
	}
}

func TestReapplyIsIdempotent(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	if _, err := setup.Apply(eng, reg, opts("opencode", "claude", "codex")); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, ws)
	rep, err := setup.Apply(eng, reg, opts("opencode", "claude", "codex"))
	if err != nil {
		t.Fatal(err)
	}
	if s := statuses(rep); s["copied"] != 0 {
		t.Errorf("re-apply copied files: %v", s)
	}
	if after := snapshot(t, ws); after != snap {
		t.Error("re-apply changed workspace files")
	}
}

func snapshot(t *testing.T, ws string) string {
	var b strings.Builder
	filepath.WalkDir(ws, func(p string, d os.DirEntry, err error) error {
		if d.IsDir() && d.Name() == ".ocws" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			b.WriteString(p + "\x00" + string(data) + "\x00")
		}
		return nil
	})
	return b.String()
}

func TestRefreshAndConflicts(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode", "claude")
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}

	// Source change on the shared body: both harness renders go stale.
	body := filepath.Join(tpl, "packs/agents/reviewer.md")
	os.WriteFile(body, []byte("---\ndescription: Reviews code\n---\n\nReview harder.\n"), 0o644)
	st := states(eng)
	if st["opencode:dev-agents"] != "refresh-available" || st["claude:dev-agents"] != "refresh-available" {
		t.Fatalf("want refresh-available, got %v", st)
	}

	// Local edit + source change = conflict; apply must block and write nothing.
	claudePath := filepath.Join(ws, ".claude/agents/reviewer.md")
	os.WriteFile(claudePath, []byte("mine\n"), 0o644)
	if st := states(eng); st["claude:dev-agents"] != "refresh-with-local-conflicts" {
		t.Fatalf("want conflicts, got %v", st)
	}
	manifestBefore := read(t, ws, ".ocws/manifest.json")
	opencodeBefore := read(t, ws, ".opencode/agents/reviewer.md")
	_, err := setup.Apply(eng, reg, o)
	if !errors.Is(err, setup.ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
	if read(t, ws, ".ocws/manifest.json") != manifestBefore || read(t, ws, ".opencode/agents/reviewer.md") != opencodeBefore {
		t.Fatal("blocked apply modified the workspace")
	}
	if read(t, ws, ".claude/agents/reviewer.md") != "mine\n" {
		t.Fatal("blocked apply clobbered the local edit")
	}

	o.Overwrite = "overwrite-approved"
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, ws, ".claude/agents/reviewer.md"), "Review harder.") {
		t.Fatal("overwrite did not refresh")
	}
	for k, s := range states(eng) {
		if s != "current" {
			t.Errorf("%s: %s", k, s)
		}
	}
}

func TestPruneStaleManagedFiles(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	// The command pack stops shipping ship.md and ships deploy.md instead.
	mf := filepath.Join(tpl, "packs/commands/manifest.json")
	os.WriteFile(filepath.Join(tpl, "packs/commands/deploy.md"), []byte("deploy\n"), 0o644)
	os.WriteFile(mf, []byte(`{"schemaVersion":3,"id":"dev-commands","componentType":"command-pack","version":"1.1.0","files":[{"source":"deploy.md","destination":".opencode/commands/deploy.md"}]}`), 0o644)

	rep, err := setup.Apply(eng, reg, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".opencode/commands/ship.md")); err != nil {
		t.Fatal("stale file removed without --prune")
	}
	stale := 0
	for _, c := range rep.Audit.Components {
		stale += len(c.StaleFiles)
	}
	if stale != 1 {
		t.Fatalf("want 1 stale file, got %d", stale)
	}
	if !strings.Contains(strings.Join(rep.Warnings, "\n"), ".opencode/commands/ship.md") {
		t.Errorf("expected untracked-stale warning, got %v", rep.Warnings)
	}
}

func TestPruneOnSameRun(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	mf := filepath.Join(tpl, "packs/commands/manifest.json")
	os.WriteFile(filepath.Join(tpl, "packs/commands/deploy.md"), []byte("deploy\n"), 0o644)
	os.WriteFile(mf, []byte(`{"schemaVersion":3,"id":"dev-commands","componentType":"command-pack","version":"1.1.0","files":[{"source":"deploy.md","destination":".opencode/commands/deploy.md"}]}`), 0o644)
	o.Prune = true
	rep, err := setup.Apply(eng, reg, o)
	if err != nil {
		t.Fatal(err)
	}
	if statuses(rep)["removed-stale"] != 1 {
		t.Fatalf("want removed-stale 1, got %v", statuses(rep))
	}
	if _, err := os.Stat(filepath.Join(ws, ".opencode/commands/ship.md")); !os.IsNotExist(err) {
		t.Fatal("stale file not pruned")
	}
}

func TestWorkspaceConfigMergeKeepsUserValues(t *testing.T) {
	tpl, ws := fixture(t)
	os.WriteFile(filepath.Join(ws, "opencode.json"), []byte("{\n  \"permission\": { \"bash\": \"allow\" },\n  \"theme\": \"x\"\n}\n"), 0o644)
	reg, eng := load(t, tpl, ws)
	rep, err := setup.Apply(eng, reg, opts("opencode"))
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, ws, "opencode.json")
	if !strings.HasPrefix(got, "{\n  \"permission\": {\n    \"bash\": \"allow\"\n  },\n  \"theme\": \"x\",\n  \"$schema\"") {
		t.Errorf("merge lost order or user values:\n%s", got)
	}
	if !strings.Contains(strings.Join(rep.Warnings, "\n"), "/permission/bash") {
		t.Errorf("expected conflict warning, got %v", rep.Warnings)
	}
}

func TestTOMLMergeConflictBlocks(t *testing.T) {
	tpl, ws := fixture(t)
	os.MkdirAll(filepath.Join(ws, ".codex"), 0o755)
	os.WriteFile(filepath.Join(ws, ".codex/config.toml"), []byte("[mcp_servers.docs]\nurl = \"https://other\"\n"), 0o644)
	reg, eng := load(t, tpl, ws)
	_, err := setup.Apply(eng, reg, opts("codex"))
	if !errors.Is(err, setup.ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".ocws/manifest.json")); err == nil {
		t.Fatal("manifest written despite block")
	}
}

func TestSelectionRules(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	o.CapabilityIDs = []string{"b1", "b2"}
	if _, err := setup.Apply(eng, reg, o); err == nil || !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("want zero-or-one error, got %v", err)
	}
	o.CapabilityIDs = []string{"nope"}
	if _, err := setup.Apply(eng, reg, o); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

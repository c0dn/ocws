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
	if oc["share"] != "disabled" || oc["mcp"].(map[string]any)["servers"].(map[string]any)["docs"] == nil {
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

	// Commands have no claude/codex target, so they are derived as skills.
	ship := read(t, ws, ".claude/skills/ship/SKILL.md")
	if !strings.HasPrefix(ship, "---\nname: ship\n") || !strings.Contains(ship, "disable-model-invocation: true") {
		t.Errorf("derived claude command-skill wrong:\n%s", ship)
	}
	if !strings.Contains(read(t, ws, ".agents/skills/ship/agents/openai.yaml"), "allow_implicit_invocation: false") {
		t.Error("derived codex skill policy missing")
	}
	_ = rep
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
	for _, c := range []struct{ harness, user, prefix, conflict string }{
		{"opencode", "{\n  \"share\": \"manual\",\n  \"theme\": \"x\"\n}\n", "{\n  \"share\": \"manual\",\n  \"theme\": \"x\",\n  \"$schema\"", "/share"},
		// V1 gets the lowered V2 template, so permissions -> permission.
		{"opencode-v1", "{\n  \"permission\": { \"bash\": \"allow\" },\n  \"theme\": \"x\"\n}\n", "{\n  \"permission\": {\n    \"bash\": \"allow\"\n  },\n  \"theme\": \"x\",\n  \"$schema\"", "/permission/bash"},
	} {
		tpl, ws := fixture(t)
		os.WriteFile(filepath.Join(ws, "opencode.json"), []byte(c.user), 0o644)
		reg, eng := load(t, tpl, ws)
		rep, err := setup.Apply(eng, reg, opts(c.harness))
		if err != nil {
			t.Fatal(err)
		}
		got := read(t, ws, "opencode.json")
		if !strings.HasPrefix(got, c.prefix) {
			t.Errorf("%s: merge lost order or user values:\n%s", c.harness, got)
		}
		if !strings.Contains(strings.Join(rep.Warnings, "\n"), c.conflict) {
			t.Errorf("%s: expected %s conflict warning, got %v", c.harness, c.conflict, rep.Warnings)
		}
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

func manifestKeys(t *testing.T, ws string) string {
	t.Helper()
	var m struct {
		Components []struct{ Harness, ID string }
	}
	json.Unmarshal([]byte(read(t, ws, ".ocws/manifest.json")), &m)
	var keys []string
	for _, c := range m.Components {
		keys = append(keys, c.Harness+":"+c.ID)
	}
	return strings.Join(keys, ",")
}

func remove(t *testing.T, eng *engine.Engine, policy string, ids ...string) *engine.InstallResult {
	t.Helper()
	keys, err := eng.ResolveInstalled(ids)
	if err != nil {
		t.Fatal(err)
	}
	res, err := eng.Uninstall(keys, nil, engine.InstallOptions{OverwritePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Blocked() {
		if _, err := eng.Write(nil, engine.WriteOptions{RemoveKeys: keys}); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func TestRemoveComponent(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	if _, err := setup.Apply(eng, reg, opts("opencode", "codex")); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.ResolveInstalled([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "installed:") {
		t.Fatalf("unknown id: %v", err)
	}

	// Bare id matches every harness; JSON and TOML fragments are backed out.
	res := remove(t, eng, "safe-refresh", "docs-mcp")
	if len(res.Components) != 2 || res.Summary["unmerged"] != 2 {
		t.Fatalf("unexpected removal: %+v", res.Summary)
	}
	if codex := read(t, ws, ".codex/config.toml"); strings.Contains(codex, "docs") || !strings.Contains(codex, "approval_policy") {
		t.Errorf("TOML unmerge wrong:\n%s", codex)
	}
	var oc map[string]any
	json.Unmarshal([]byte(read(t, ws, "opencode.json")), &oc)
	if strings.Contains(read(t, ws, "opencode.json"), `"docs"`) {
		t.Errorf("docs MCP still merged: %v", oc)
	}
	if oc["share"] != "disabled" {
		t.Errorf("unrelated config lost: %v", oc)
	}
	if strings.Contains(manifestKeys(t, ws), "docs-mcp") {
		t.Error("docs-mcp still in manifest")
	}

	// Edited file blocks and changes nothing; overwrite-approved removes it.
	agent := filepath.Join(ws, ".opencode/agents/reviewer.md")
	os.WriteFile(agent, []byte("mine\n"), 0o644)
	before := manifestKeys(t, ws)
	if res := remove(t, eng, "safe-refresh", "opencode:dev-agents"); !res.Blocked() {
		t.Fatal("edited file did not block")
	}
	if manifestKeys(t, ws) != before || read(t, ws, ".opencode/agents/reviewer.md") != "mine\n" {
		t.Fatal("blocked removal changed the workspace")
	}
	remove(t, eng, "overwrite-approved", "opencode:dev-agents")
	if _, err := os.Stat(agent); !os.IsNotExist(err) {
		t.Fatal("agent not removed")
	}
	if _, err := os.Stat(filepath.Dir(agent)); !os.IsNotExist(err) {
		t.Error("empty agents dir left behind")
	}
	if !strings.Contains(manifestKeys(t, ws), "codex:dev-agents") || strings.Contains(manifestKeys(t, ws), "opencode:dev-agents") {
		t.Errorf("wrong manifest after removal: %s", manifestKeys(t, ws))
	}
}

func TestPruneDroppedComponents(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	o.CapabilityIDs = []string{}
	rep, err := setup.Apply(eng, reg, o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Removed != nil || !strings.Contains(read(t, ws, "opencode.json"), `"docs"`) || !strings.Contains(manifestKeys(t, ws), "docs-mcp") {
		t.Fatal("dropped component removed without --prune")
	}
	o.Prune = true
	if rep, err = setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	if rep.Removed == nil || rep.Removed.Summary["unmerged"] != 1 {
		t.Fatalf("prune did not uninstall: %+v", rep.Removed)
	}
	if strings.Contains(read(t, ws, "opencode.json"), `"docs"`) || strings.Contains(manifestKeys(t, ws), "docs-mcp") {
		t.Fatal("docs-mcp not uninstalled")
	}
}

func TestPrunePackDeletedFromTemplates(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	// Delete the pack and its profile entry from the templates.
	os.RemoveAll(filepath.Join(tpl, "packs/mcp"))
	pf := filepath.Join(tpl, "profiles.json")
	b, _ := os.ReadFile(pf)
	os.WriteFile(pf, []byte(strings.Replace(string(b), `{ "id": "docs-mcp", "manifest": "packs/mcp/manifest.json", "recommendWhen": { "paths": ["**/*.ipynb"] } },`, "", 1)), 0o644)
	reg, eng = load(t, tpl, ws)
	o.CapabilityIDs = nil

	rep, err := setup.Apply(eng, reg, withPrune(o))
	if err != nil {
		t.Fatalf("prune of deleted pack failed: %v (removed=%+v)", err, rep.Removed)
	}
	if strings.Contains(read(t, ws, "opencode.json"), `"docs"`) || strings.Contains(manifestKeys(t, ws), "docs-mcp") {
		t.Fatal("deleted pack not uninstalled")
	}
}

func withPrune(o setup.Options) setup.Options { o.Prune = true; return o }

func TestForceRemoveEditedDroppedComponent(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	// Edit the merged key, then drop the capability.
	oc := strings.Replace(read(t, ws, "opencode.json"), "https://docs.example/mcp", "https://mine", 1)
	os.WriteFile(filepath.Join(ws, "opencode.json"), []byte(oc), 0o644)
	o.CapabilityIDs = []string{}
	o.Prune = true
	if _, err := setup.Apply(eng, reg, o); !errors.Is(err, setup.ErrBlocked) {
		t.Fatalf("edited key removed without force: %v", err)
	}
	if read(t, ws, "opencode.json") != oc {
		t.Fatal("blocked prune changed opencode.json")
	}
	o.ForceRemove = true
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, ws, "opencode.json"), `"docs"`) || strings.Contains(manifestKeys(t, ws), "docs-mcp") {
		t.Fatal("force remove did not uninstall")
	}
}

func TestOpenCodeV1LowersV2Templates(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode-v1")
	o.CapabilityIDs = []string{"docs-mcp", "shell-tool"}
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	agent := read(t, ws, ".opencode/agents/reviewer.md")
	for _, want := range []string{"model: anthropic/claude-sonnet-4-5\n", "variant: high\n", "permission:\n", "read: \"allow\"", "bash:\n", "\"*\": \"ask\"", "\"git diff *\": \"allow\""} {
		if !strings.Contains(agent, want) {
			t.Errorf("lowered agent missing %q:\n%s", want, agent)
		}
	}
	if strings.Contains(agent, "permissions:") || strings.Contains(agent, "#high") {
		t.Errorf("V2 fields left in agent:\n%s", agent)
	}
	var oc map[string]any
	json.Unmarshal([]byte(read(t, ws, "opencode.json")), &oc)
	docs, _ := oc["mcp"].(map[string]any)["docs"].(map[string]any)
	if docs == nil || docs["enabled"] != true || docs["disabled"] != nil {
		t.Errorf("MCP not lowered to flat V1 shape: %v", oc["mcp"])
	}
	if p, _ := oc["permission"].(map[string]any); p["bash"] != "ask" || oc["permissions"] != nil {
		t.Errorf("workspace config not lowered: %v", oc)
	}
	if _, err := os.Stat(filepath.Join(ws, ".opencode/tools/shell.ts")); err != nil {
		t.Error("custom tool not installed for V1")
	}
	for k, st := range states(eng) {
		if st != "current" {
			t.Errorf("%s: %s", k, st)
		}
	}
	// Removal backs the lowered key out.
	remove(t, eng, "safe-refresh", "docs-mcp")
	if strings.Contains(read(t, ws, "opencode.json"), `"docs"`) {
		t.Error("lowered MCP key not removed")
	}
	if _, err := setup.Apply(eng, reg, opts("opencode", "opencode-v1")); err == nil || !strings.Contains(err.Error(), "select only one") {
		t.Errorf("V1+V2 accepted: %v", err)
	}
}

// Every harness installs the V2 test templates without explicit targets:
// derived files land where the harness reads them, and re-apply, status and
// remove all behave.
func TestEveryHarnessDerives(t *testing.T) {
	cases := map[string][]string{
		"gemini":  {".gemini/agents/reviewer.md", ".gemini/commands/ship.toml", ".agents/skills/review/SKILL.md", ".gemini/settings.json", "GEMINI.md"},
		"qwen":    {".qwen/agents/reviewer.md", ".qwen/commands/ship.md", ".qwen/skills/review/SKILL.md", ".qwen/settings.json"},
		"copilot": {".github/agents/reviewer.agent.md", ".agents/skills/ship/SKILL.md", ".agents/skills/review/SKILL.md", ".mcp.json"},
		"cursor":  {".cursor/agents/reviewer.md", ".agents/skills/ship/SKILL.md", ".cursor/mcp.json"},
		"droid":   {".factory/droids/reviewer.md", ".factory/commands/ship.md", ".agents/skills/review/SKILL.md", ".factory/mcp.json"},
		"kiro":    {".kiro/agents/reviewer.md", ".kiro/prompts/ship.md", ".kiro/skills/review/SKILL.md", ".kiro/settings/mcp.json"},
		"amp":     {".agents/skills/ship/SKILL.md", ".agents/skills/review/SKILL.md", ".amp/settings.json"},
		"crush":   {".crush/commands/ship.md", ".agents/skills/review/SKILL.md", ".crushrc"},
		"goose":   {".agents/agents/reviewer.md", ".agents/skills/ship/SKILL.md", ".agents/skills/review/SKILL.md"},
		"cline":   {".agents/skills/ship/SKILL.md", ".agents/skills/review/SKILL.md"},
		"kilo":    {".kilo/agents/reviewer.md", ".kilo/commands/ship.md", ".kilo/skills/review/SKILL.md", "kilo.json"},
		"pi":      {".pi/prompts/ship.md", ".agents/skills/review/SKILL.md", ".pi/mcp.json"},
		"hermes":  {".agents/skills/ship/SKILL.md", ".agents/skills/review/SKILL.md"},
	}
	for h, files := range cases {
		t.Run(h, func(t *testing.T) {
			tpl, ws := fixture(t)
			reg, eng := load(t, tpl, ws)
			o := opts(h)
			if _, err := setup.Apply(eng, reg, o); err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
					t.Errorf("missing %s", f)
				}
			}
			for k, st := range states(eng) {
				if st != "current" {
					t.Errorf("%s: %s", k, st)
				}
			}
			snap := snapshot(t, ws)
			if _, err := setup.Apply(eng, reg, o); err != nil || snapshot(t, ws) != snap {
				t.Errorf("re-apply not idempotent: %v", err)
			}
			if strings.Contains(read(t, ws, ".ocws/manifest.json"), "docs-mcp") {
				remove(t, eng, "safe-refresh", "docs-mcp")
				for _, f := range files {
					if strings.HasSuffix(f, ".json") {
						if b, err := os.ReadFile(filepath.Join(ws, f)); err == nil && strings.Contains(string(b), `"docs"`) {
							t.Errorf("%s still has docs MCP after remove", f)
						}
					}
				}
			}
		})
	}
}

func TestSharedMCPAndSkills(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	// Claude (explicit target) and Copilot (derived) share .mcp.json; codex,
	// gemini and pi share .agents/skills.
	if _, err := setup.Apply(eng, reg, opts("claude", "codex", "copilot", "gemini", "pi")); err != nil {
		t.Fatal(err)
	}
	remove(t, eng, "safe-refresh", "copilot:docs-mcp", "pi:dev-skills")
	if !strings.Contains(read(t, ws, ".mcp.json"), `"docs"`) {
		t.Error("removing copilot MCP removed claude's shared entry")
	}
	if _, err := os.Stat(filepath.Join(ws, ".agents/skills/review/SKILL.md")); err != nil {
		t.Error("removing pi skills removed the shared .agents/skills copy")
	}
	remove(t, eng, "safe-refresh", "claude:docs-mcp")
	if strings.Contains(read(t, ws, ".mcp.json"), `"docs"`) {
		t.Error("last owner removed but .mcp.json still has docs")
	}
}

// Harnesses write their own keys into shared config files (Qwen adds
// "$version", Kilo adds "$schema"); only ocws's merged keys decide status.
func TestHarnessKeysInMergedConfigStayCurrent(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	if _, err := setup.Apply(eng, reg, opts("qwen")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(ws, ".qwen/settings.json")
	os.WriteFile(p, []byte(strings.Replace(read(t, ws, ".qwen/settings.json"), "{", "{\n  \"$version\": 4,", 1)), 0o644)
	if st := states(eng)["qwen:docs-mcp"]; st != "current" {
		t.Fatalf("harness-added key flagged: %s", st)
	}
	os.WriteFile(p, []byte(strings.Replace(read(t, ws, ".qwen/settings.json"), "https://docs.example/mcp", "https://mine", 1)), 0o644)
	if st := states(eng)["qwen:docs-mcp"]; st == "current" {
		t.Fatal("edited merged key not flagged")
	}
}

// OpenCode V2 no longer loads .opencode/tools; each V1 tool file gets a
// generated V2 plugin that registers it. V1 loads the files natively.
func TestOpenCodeV2ToolShims(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	o := opts("opencode")
	o.CapabilityIDs = []string{"shell-tool"}
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	shim := read(t, ws, ".opencode/plugins/ocws-tool-shell.ts")
	for _, want := range []string{`const FILE = "shell"`, `id: "ocws-tool-" + FILE`, "ctx.tool.transform", `io: "input"`, "@opencode-ai/plugin@1"} {
		if !strings.Contains(shim, want) {
			t.Errorf("shim missing %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, ".opencode/tools/shell.ts")); err != nil {
		t.Error("V1 tool file not installed next to the shim")
	}
	for k, st := range states(eng) {
		if st != "current" {
			t.Errorf("%s: %s", k, st)
		}
	}
	remove(t, eng, "safe-refresh", "shell-tool")
	if _, err := os.Stat(filepath.Join(ws, ".opencode/plugins")); !os.IsNotExist(err) {
		t.Error("shim not removed with its pack")
	}

	tpl, ws = fixture(t)
	reg, eng = load(t, tpl, ws)
	o = opts("opencode-v1")
	o.CapabilityIDs = []string{"shell-tool"}
	if _, err := setup.Apply(eng, reg, o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".opencode/plugins")); !os.IsNotExist(err) {
		t.Error("V1 got V2 tool shims")
	}
}

// Crush's current config is Bash (.crushrc): MCP servers become `mcp add`
// lines appended as one block; user lines around it are left alone and
// removal takes out exactly that block.
func TestCrushrc(t *testing.T) {
	tpl, ws := fixture(t)
	os.WriteFile(filepath.Join(ws, ".crushrc"), []byte("# mine\npermissions allow view\n"), 0o644)
	reg, eng := load(t, tpl, ws)
	if _, err := setup.Apply(eng, reg, opts("crush")); err != nil {
		t.Fatal(err)
	}
	rc := read(t, ws, ".crushrc")
	if !strings.HasPrefix(rc, "# mine\npermissions allow view\n\n") || !strings.Contains(rc, "mcp add 'docs' --type http --url 'https://docs.example/mcp'\n") {
		t.Fatalf(".crushrc wrong:\n%s", rc)
	}
	os.WriteFile(filepath.Join(ws, ".crushrc"), []byte(rc+"option debug true\n"), 0o644)
	if st := states(eng)["crush:docs-mcp"]; st != "current" {
		t.Errorf("user line flagged the merged block: %s", st)
	}
	remove(t, eng, "safe-refresh", "crush:docs-mcp")
	if got := read(t, ws, ".crushrc"); got != "# mine\npermissions allow view\n\noption debug true\n" {
		t.Errorf("remove left:\n%q", got)
	}
}

func TestCodexRemovalProtectsUserTOML(t *testing.T) {
	for _, policy := range []string{"safe-refresh", "overwrite-approved"} {
		t.Run(policy, func(t *testing.T) {
			tpl, ws := fixture(t)
			reg, eng := load(t, tpl, ws)
			o := opts("codex")
			if _, err := setup.Apply(eng, reg, o); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(ws, ".codex/config.toml")
			edited := read(t, ws, ".codex/config.toml") + "bearer_token_env_var = 'MY_SECRET'\n"
			if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
				t.Fatal(err)
			}
			if st := states(eng)["codex:docs-mcp"]; st != "locally-modified" {
				t.Fatalf("edited managed table state: %s", st)
			}
			before := read(t, ws, ".ocws/manifest.json")
			keys, err := eng.ResolveInstalled([]string{"codex:docs-mcp"})
			if err != nil {
				t.Fatal(err)
			}
			res, err := eng.Uninstall(keys, nil, engine.InstallOptions{OverwritePolicy: policy, DryRun: true})
			if err != nil || !res.Blocked() {
				t.Fatalf("unsafe dry-run allowed: %+v, %v", res, err)
			}
			if res := remove(t, eng, policy, "codex:docs-mcp"); !res.Blocked() {
				t.Fatal("edited TOML table did not block removal")
			}
			o.CapabilityIDs = []string{}
			o.Prune, o.ForceRemove = true, policy == "overwrite-approved"
			if _, err := setup.Apply(eng, reg, o); !errors.Is(err, setup.ErrBlocked) {
				t.Fatalf("edited TOML table did not block prune: %v", err)
			}
			if read(t, ws, ".codex/config.toml") != edited || read(t, ws, ".ocws/manifest.json") != before {
				t.Fatal("blocked removal changed user config or manifest")
			}
		})
	}
}

func TestCodexRemovalPreservesOtherTables(t *testing.T) {
	tpl, ws := fixture(t)
	reg, eng := load(t, tpl, ws)
	if _, err := setup.Apply(eng, reg, opts("codex")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws, ".codex/config.toml")
	extra := "\n[mcp_servers.mine]\nurl = 'https://mine.example/mcp'\nnote = '''one\n\n\ntwo'''\n"
	if err := os.WriteFile(path, []byte(read(t, ws, ".codex/config.toml")+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := states(eng)["codex:docs-mcp"]; st != "current" {
		t.Fatalf("unrelated table flagged: %s", st)
	}
	if res := remove(t, eng, "safe-refresh", "codex:docs-mcp"); res.Blocked() {
		t.Fatalf("unchanged table blocked: %+v", res.Summary)
	}
	got := read(t, ws, ".codex/config.toml")
	if !strings.Contains(got, extra) || strings.Contains(got, "[mcp_servers.docs]") {
		t.Fatalf("removal changed unrelated TOML: %q", got)
	}
}

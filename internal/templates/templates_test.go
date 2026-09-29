package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c0dn/ocws/internal/registry"
	"github.com/pelletier/go-toml/v2"
)

func TestFixtureValidates(t *testing.T) {
	reg, err := registry.Load("../../testdata/templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range Validate(reg) {
		if i.Level == "error" {
			t.Errorf("%s: %s", i.Where, i.Message)
		}
	}
}

func TestPortTranslatesPack(t *testing.T) {
	dir := t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	write("a.md", "---\ndescription: Does A\nmode: primary\n---\n\nBody\n")
	write("cmd.md", "---\ndescription: Run it\n---\n\nRun $1\n")
	write("sk/SKILL.md", "---\nname: sk\ndescription: skill\n---\n")
	write("frag.json", `{"mcp":{"servers":{"j":{"type":"local","command":["uvx","j@1"],"environment":{"TOKEN":"{env:TOKEN}","MODE":"x"}},"r":{"type":"remote","url":"https://r","headers":{"Authorization":"Bearer {env:RK}"}}}}}`)
	write("manifest.json", `{"schemaVersion":2,"id":"p","componentType":"agent-pack","version":"1.0.0","files":[
	 {"source":"a.md","destination":".opencode/agents/a.md"},
	 {"source":"cmd.md","destination":".opencode/commands/cmd.md"},
	 {"source":"sk","destination":".opencode/skills/sk"},
	 {"source":"frag.json","destination":"opencode.json","installMode":"merge","jsonPointers":["/mcp/servers/j","/mcp/servers/r","/permissions"]}]}`)
	res, err := Port(filepath.Join(dir, "manifest.json"), []string{"claude", "codex"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Files != 4 || res[1].Files != 5 {
		t.Fatalf("results %+v", res)
	}
	pm, err := registry.LoadPack(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pm.SupportedHarnesses(), ",") != "opencode,claude,codex" {
		t.Errorf("harnesses %v", pm.SupportedHarnesses())
	}
	for _, i := range validatePack(pm) {
		t.Errorf("%s: %s", i.Where, i.Message)
	}
	claudeMCP, _ := os.ReadFile(filepath.Join(dir, "harness/claude/frag.mcp.json"))
	for _, want := range []string{`"${TOKEN}"`, `"type": "http"`, `"Bearer ${RK}"`, `"args"`} {
		if !strings.Contains(string(claudeMCP), want) {
			t.Errorf("claude mcp missing %s:\n%s", want, claudeMCP)
		}
	}
	codexMCP, _ := os.ReadFile(filepath.Join(dir, "harness/codex/frag.mcp.toml"))
	var cfg struct {
		MCP map[string]map[string]any `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(codexMCP, &cfg); err != nil {
		t.Fatalf("codex fragment invalid: %v\n%s", err, codexMCP)
	}
	if cfg.MCP["r"]["bearer_token_env_var"] != "RK" || cfg.MCP["j"]["command"] != "uvx" {
		t.Errorf("codex translation wrong: %v", cfg.MCP)
	}
	if again, _ := Port(filepath.Join(dir, "manifest.json"), []string{"claude"}, false); !again[0].Skipped {
		t.Error("existing target regenerated without --force")
	}
	warn := strings.Join(append(res[0].Warnings, res[1].Warnings...), "\n")
	for _, want := range []string{"primary agent", "$0", "/permissions"} {
		if !strings.Contains(warn, want) {
			t.Errorf("missing warning %q in:\n%s", want, warn)
		}
	}
}

func TestImportRewritesLegacyRegistry(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "packs/x"), 0o755)
	os.WriteFile(filepath.Join(src, "packs/x/manifest.json"), []byte(`{"schemaVersion":2,"id":"x","componentType":"skill-pack","files":[{"source":"manifest.json","destination":".opencode/x.json"}]}`), 0o644)
	os.WriteFile(filepath.Join(src, "workspace-profiles.json"), []byte(`{"schemaVersion":2,"profiles":{"p":{"basePacks":[{"id":"x","manifest":"templates/packs/x/manifest.json"}]}}}`), 0o644)
	dest := filepath.Join(t.TempDir(), "t")
	if _, err := Import(src, dest); err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Load(dest)
	if err != nil || reg.SchemaVersion != 3 || reg.Profiles["p"].BasePacks[0].Manifest != "packs/x/manifest.json" {
		t.Fatalf("import wrong: %+v %v", reg, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "workspace-profiles.json")); !os.IsNotExist(err) {
		t.Error("legacy registry left behind")
	}
}

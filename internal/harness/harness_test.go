package harness

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestTOMLMultilineRoundTrips(t *testing.T) {
	for _, body := range []string{
		"plain\n", `back\slash C:\tmp` + "\n", "has ''' triple\n", `quote at end"`, "ends with '", "mixed ''' and \"\"\" and \\\n",
	} {
		var out map[string]string
		if err := toml.Unmarshal([]byte("v = "+TOMLMultiline(body)), &out); err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if out["v"] != body {
			t.Errorf("round trip %q -> %q", body, out["v"])
		}
	}
}

func TestRenderReplacesFrontmatter(t *testing.T) {
	body := []byte("---\ndescription: x\nmode: primary\n---\n\n# Body\n")
	out, err := Render(RenderFrontmatter, body, []byte("name: a\ndescription: b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "---\nname: a\ndescription: b\n---\n\n# Body\n" {
		t.Errorf("got %q", out)
	}
	if _, err := Render(RenderCodexAgent, body, []byte("name = 'a'\ndeveloper_instructions = 'x'\n")); err == nil {
		t.Error("header setting developer_instructions accepted")
	}
	if _, err := Render(RenderFrontmatter, body, []byte("a: [unclosed\n")); err == nil {
		t.Error("invalid YAML header accepted")
	}
}

func TestSplitFrontmatterWithoutBlock(t *testing.T) {
	front, body := SplitFrontmatter([]byte("# no front\n---\n"))
	if front != nil || !strings.HasPrefix(string(body), "# no front") {
		t.Errorf("front=%q body=%q", front, body)
	}
}

func TestExpandTokens(t *testing.T) {
	codex, _ := Get("codex")
	if got, _ := codex.Expand("{skills}/x/SKILL.md"); got != ".agents/skills/x/SKILL.md" {
		t.Errorf("got %s", got)
	}
	if _, err := codex.Expand("{tools}/x.ts"); err == nil {
		t.Error("codex {tools} should not exist")
	}
	if _, err := ParseList([]string{"opencode,nope"}); err == nil {
		t.Error("unknown harness accepted")
	}
	if l, _ := ParseList([]string{"codex", "opencode,claude"}); strings.Join(l, ",") != "opencode,claude,codex" {
		t.Errorf("order %v", l)
	}
}

func TestLowerConfig(t *testing.T) {
	in := []byte(`{"permissions":[{"action":"shell","resource":"*","effect":"ask"},{"action":"shell","resource":"git *","effect":"allow"},{"action":"edit","resource":"*","effect":"deny"},{"action":"subagent","resource":"*","effect":"allow"}],
"mcp":{"servers":{"a":{"type":"local","command":["x"],"disabled":true,"codemode":true},"b":{"type":"remote","url":"u","oauth":{"client_id":"c"}}}},
"plugins":[{"package":"p","options":{"k":1}},"q"],"skills":["./s","https://x/y"],"snapshots":false,"model":"a/b#high","default_agent":"build"}`)
	out, err := LowerConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"permission":{"bash":{"*":"ask","git*":"allow"},"edit":"deny","task":"allow"},"mcp":{"a":{"type":"local","command":["x"],"enabled":false},"b":{"type":"remote","url":"u","oauth":{"clientId":"c"}}},"plugin":[["p",{"k":1}],"q"],"skills":{"paths":["./s"],"urls":["https://x/y"]},"snapshot":false,"model":"a/b","default_agent":"build"}`
	if got := strings.Join(strings.Fields(string(out)), ""); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	for in, want := range map[string]string{"/mcp/servers/x": "/mcp/x", "/permissions": "/permission", "/agents/a": "/agent/a", "/share": "/share"} {
		if got := LowerPointer(in); got != want {
			t.Errorf("LowerPointer(%s) = %s", in, got)
		}
	}
}

func TestDerivedRenders(t *testing.T) {
	agent := []byte("---\ndescription: Reviews code\nmode: subagent\n---\n\nReview.\n")
	cases := []struct{ mode, dest, want string }{
		{"agent:claude", ".claude/agents/rev.md", "---\nname: rev\ndescription: Reviews code\nmodel: inherit\n---\n\nReview.\n"},
		{"command:claude", ".claude/skills/rev/SKILL.md", "---\nname: rev\ndescription: Reviews code\ndisable-model-invocation: true\n---\n\nReview.\n"},
	}
	for _, c := range cases {
		out, err := RenderFile(c.mode, agent, nil, c.dest)
		if err != nil || string(out) != c.want {
			t.Errorf("%s: %v\n%q", c.mode, err, out)
		}
	}
	out, err := RenderFile("agent:codex", agent, nil, ".codex/agents/rev.toml")
	var v map[string]any
	if err != nil || toml.Unmarshal(out, &v) != nil || v["name"] != "rev" || v["developer_instructions"] != "Review.\n" {
		t.Errorf("codex agent: %v %s", err, out)
	}
	mcp := []byte(`{"mcp":{"servers":{"d":{"type":"local","command":["npx","-y","srv"],"environment":{"K":"{env:K}"}}}}}`)
	out, err = RenderFile("mcp:claude", mcp, nil, ".mcp.json")
	if err != nil || strings.Join(strings.Fields(string(out)), "") != `{"mcpServers":{"d":{"type":"stdio","command":"npx","args":["-y","srv"],"env":{"K":"${K}"}}}}` {
		t.Errorf("claude mcp: %v %s", err, out)
	}
	out, err = RenderFile("mcp:codex", mcp, nil, ".codex/config.toml")
	if err != nil || !strings.Contains(string(out), "[mcp_servers.d]") {
		t.Errorf("codex mcp: %v %s", err, out)
	}
}

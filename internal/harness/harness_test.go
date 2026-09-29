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

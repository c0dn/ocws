package jsonx

import (
	"os/exec"
	"strings"
	"testing"
)

func TestMarshalMatchesJSONStringify(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src := `{"z":1,"a":{"b":[],"c":{},"d":[1,2.50,{"e":"<tag> & \"q\" é \u2028 \u0001\t\\"}]},"n":null,"t":true,"big":12345678901234567890}`
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// JSON.stringify would round 12345678901234567890 and 2.50; compare the
	// parts it keeps exactly by normalising those two numbers first.
	got := strings.NewReplacer("12345678901234567890", "1", "2.50", "2.5").Replace(string(Marshal(v)))
	norm := strings.NewReplacer("12345678901234567890", "1", "2.50", "2.5").Replace(src)
	out, err := exec.Command(node, "-e", "process.stdout.write(JSON.stringify(JSON.parse(process.argv[1]),null,2))", norm).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got != string(out) {
		t.Errorf("go:\n%s\nnode:\n%s", got, out)
	}
	if !strings.Contains(string(Marshal(v)), "12345678901234567890") {
		t.Error("number text not preserved")
	}
}

func TestPermissionsMergePutsPackRulesFirst(t *testing.T) {
	dest := `{"permissions":[{"action":"read","effect":"allow"},{"action":"x","effect":"deny"}]}`
	frag := `{"permissions":[{"action":"x","effect":"ask"},{"action":"read","effect":"allow"}]}`
	out, err := MergeFragmentBytes([]byte(frag), []byte(dest), []string{"/permissions"})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := Parse(out)
	rules := v.(*Object).Values["permissions"].([]any)
	if len(rules) != 3 {
		t.Fatalf("want 3 rules (dedupe), got %d", len(rules))
	}
	if rules[0].(*Object).Values["effect"] != "ask" || rules[2].(*Object).Values["effect"] != "deny" {
		t.Errorf("workspace rule must stay last: %s", out)
	}
	again, _ := MergeFragmentBytes([]byte(frag), out, []string{"/permissions"})
	if string(again) != string(out) {
		t.Error("permissions merge is not idempotent")
	}
}

func TestMergePointerKeepsSiblings(t *testing.T) {
	dest := `{"mcp":{"servers":{"existing":{"url":"a"}}},"k":1}`
	frag := `{"mcp":{"servers":{"new":{"url":"b"}}}}`
	out, err := MergeFragmentBytes([]byte(frag), []byte(dest), []string{"/mcp/servers/new"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"existing"`) || !strings.Contains(s, `"new"`) || strings.Index(s, `"mcp"`) > strings.Index(s, `"k"`) {
		t.Errorf("siblings or order lost:\n%s", s)
	}
	if _, err := MergeFragmentBytes([]byte(frag), []byte(dest), []string{"/mcp/missing"}); err == nil {
		t.Error("missing pointer accepted")
	}
}

func TestFillMissingReportsConflicts(t *testing.T) {
	e, _ := Parse([]byte(`{"permission":{"bash":"allow"},"a":1}`))
	i, _ := Parse([]byte(`{"permission":{"bash":"ask","edit":"ask"},"b":2}`))
	c := FillMissing(e.(*Object), i.(*Object), "")
	if len(c) != 1 || c[0] != "/permission/bash" {
		t.Errorf("conflicts = %v", c)
	}
	if got := string(Marshal(e)); !strings.Contains(got, `"edit": "ask"`) || !strings.Contains(got, `"bash": "allow"`) {
		t.Errorf("fill wrong: %s", got)
	}
}

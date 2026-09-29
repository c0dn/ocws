package paths

import (
	"path/filepath"
	"testing"
)

func TestHomeResolution(t *testing.T) {
	t.Setenv("HOME", "/h")
	t.Setenv("OCWS_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, _ := Home(""); got != filepath.FromSlash("/h/.config/ocws") {
		t.Errorf("default = %s", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if got, _ := Home(""); got != filepath.FromSlash("/x/ocws") {
		t.Errorf("xdg = %s", got)
	}
	t.Setenv("OCWS_HOME", "/o")
	if got, _ := Home(""); got != filepath.FromSlash("/o") {
		t.Errorf("env = %s", got)
	}
	if got, _ := Home("/f"); got != filepath.FromSlash("/f") {
		t.Errorf("flag = %s", got)
	}
}

func TestTemplatesRootAndConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OCWS_TEMPLATES", "")
	if got, _ := TemplatesRoot(home, "", Config{}); got != filepath.Join(home, "templates") {
		t.Errorf("default = %s", got)
	}
	if got, _ := TemplatesRoot(home, "", Config{Templates: "tpl"}); got != filepath.Join(home, "tpl") {
		t.Errorf("relative config = %s", got)
	}
	if err := SaveConfig(home, Config{Source: "git@x:y.git", DefaultHarnesses: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(home)
	if err != nil || c.Source != "git@x:y.git" || c.DefaultHarnesses[0] != "claude" {
		t.Errorf("round trip: %+v %v", c, err)
	}
}

package engine

import (
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, c := range [][3]string{{"1.0.0", "1.0.1", "source-newer"}, {"v2", "1.9", "installed-newer"}, {"1.0", "1.0.0", "same"}, {"1.0-beta", "1.0", "different"}, {"", "1", "unknown"}} {
		if got := CompareVersions(c[0], c[1]); got != c[2] {
			t.Errorf("%s vs %s = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

func TestMergeTOMLFragment(t *testing.T) {
	dest := []byte("# keep\nmodel = \"x\" # me\n\n[mcp_servers.a]\ncommand = \"a\"\n")
	frag := []byte("[mcp_servers.b]\nurl = \"https://b\"\n")
	out, err := MergeTOMLFragment(frag, dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), string(dest)) || !strings.Contains(string(out), "[mcp_servers.b]") {
		t.Errorf("merge lost content:\n%s", out)
	}
	again, err := MergeTOMLFragment(frag, out)
	if err != nil || string(again) != string(out) {
		t.Errorf("not idempotent: %v", err)
	}
	if _, err := MergeTOMLFragment([]byte("[mcp_servers.a]\ncommand = \"z\"\n"), dest); err == nil {
		t.Error("conflicting table accepted")
	}
	if out, err := MergeTOMLFragment(frag, nil); err != nil || string(out) != string(frag) {
		t.Errorf("missing destination: %q %v", out, err)
	}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		policy, dest, prevInst, prevSrc, want string
		exists, hasPrev                       bool
	}{
		{"safe-refresh", "", "", "", "copied", false, false},
		{"safe-refresh", "S", "", "", "adopted-existing", true, false},
		{"safe-refresh", "X", "", "", "blocked-conflict", true, false},
		{"overwrite-approved", "X", "", "", "copied", true, false},
		{"safe-refresh", "OLD", "OLD", "OLD", "copied", true, true},
		{"missing-only", "OLD", "OLD", "OLD", "blocked-conflict", true, true},
		{"safe-refresh", "EDIT", "OLD", "OLD", "blocked-conflict", true, true},
		{"overwrite-approved", "EDIT", "OLD", "OLD", "copied", true, true},
	}
	for _, c := range cases {
		d := decide(c.policy, c.hasPrev, "S", c.exists, c.dest, c.prevInst, c.prevSrc)
		if d.status != c.want {
			t.Errorf("%+v -> %s", c, d.status)
		}
	}
}

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

func TestUnmergeTOMLFragment(t *testing.T) {
	fragment := "[mcp_servers.docs]\nurl = 'https://docs.example/mcp'\n"
	for _, tc := range []struct {
		name, dest, want string
		blocked          bool
	}{
		{"only managed table", fragment, "\n", false},
		{"unrelated tables", "# mine\nmodel = 'x'\n\n" + fragment + "\n[mcp_servers.other]\ncommand = 'other'\n", "# mine\nmodel = 'x'\n\n\n\n[mcp_servers.other]\ncommand = 'other'\n", false},
		{"added credential", fragment + "bearer_token_env_var = 'MY_SECRET'\n", "", true},
		{"added subtable", fragment + "[mcp_servers.docs.http_headers]\nAuthorization = 'secret'\n", "", true},
		{"edited URL", strings.Replace(fragment, "docs.example", "mine.example", 1), "", true},
		{"invalid destination", fragment + "broken = [\n", "", true},
		{"matching string before table", "note = '''\n" + fragment + "'''\n" + fragment, "", true},
		{"preserve multiline whitespace", "note = '''one\n\n\ntwo'''\n" + fragment, "note = '''one\n\n\ntwo'''\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := UnmergeTOMLFragment([]byte(fragment), []byte(tc.dest))
			if tc.blocked {
				if err == nil {
					t.Fatalf("unsafe removal accepted: %s", out)
				}
				return
			}
			if err != nil || string(out) != tc.want {
				t.Fatalf("got %q, %v; want %q", out, err, tc.want)
			}
		})
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

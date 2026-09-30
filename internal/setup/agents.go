package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/c0dn/ocws/internal/harness"
	"github.com/c0dn/ocws/internal/jsonx"
	"github.com/c0dn/ocws/internal/registry"
)

type AgentsInfo struct {
	Name        string
	Description string
	Conventions []string
}

var firstHeading = regexp.MustCompile(`(?m)^# .*$`)

// RenderAgentsMD builds AGENTS.md from the profile guide (a workspace
// AGENTS template) plus local facts. Without a guide it stays minimal.
func RenderAgentsMD(ws string, reg *registry.Registry, profile *registry.Profile, info AgentsInfo) (string, error) {
	name := strings.TrimSpace(info.Name)
	if name == "" {
		name = filepath.Base(ws)
	}
	var b strings.Builder
	guide := ""
	if profile != nil && profile.Guide != "" {
		data, err := os.ReadFile(reg.Resolve(profile.Guide))
		if err != nil {
			return "", fmt.Errorf("profile guide %s: %w", profile.Guide, err)
		}
		guide = string(data)
	}
	heading := "# " + name
	if guide != "" && firstHeading.MatchString(guide) {
		loc := firstHeading.FindStringIndex(guide)
		b.WriteString(heading + "\n")
		if d := strings.TrimSpace(info.Description); d != "" {
			b.WriteString("\n" + d + "\n")
		}
		if rest := strings.Trim(guide[loc[1]:], "\n"); rest != "" {
			b.WriteString("\n" + rest + "\n")
		}
	} else {
		b.WriteString(heading + "\n")
		if d := strings.TrimSpace(info.Description); d != "" {
			b.WriteString("\n" + d + "\n")
		}
		if guide != "" {
			b.WriteString("\n" + strings.TrimRight(guide, "\n") + "\n")
		}
	}
	if cmds := DetectCommands(ws); len(cmds) > 0 && !strings.Contains(b.String(), "## Commands") {
		b.WriteString("\n## Commands\n\n```bash\n" + strings.Join(cmds, "\n") + "\n```\n")
	}
	var conv []string
	for _, c := range info.Conventions {
		if c = strings.TrimSpace(c); c != "" {
			conv = append(conv, "- "+strings.TrimPrefix(c, "- "))
		}
	}
	if len(conv) > 0 {
		b.WriteString("\n## Conventions\n\n" + strings.Join(conv, "\n") + "\n")
	}
	return b.String(), nil
}

// DetectCommands infers common build/test commands from project markers.
func DetectCommands(ws string) []string {
	has := func(p string) bool { _, err := os.Stat(filepath.Join(ws, p)); return err == nil }
	var out []string
	if has("package.json") {
		runner := "npm run"
		switch {
		case has("bun.lock") || has("bun.lockb"):
			runner = "bun run"
		case has("pnpm-lock.yaml"):
			runner = "pnpm"
		case has("yarn.lock"):
			runner = "yarn"
		}
		if data, err := os.ReadFile(filepath.Join(ws, "package.json")); err == nil {
			var pkg struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(data, &pkg) == nil {
				var names []string
				for k := range pkg.Scripts {
					names = append(names, k)
				}
				sort.Strings(names)
				for _, k := range names {
					if len(out) >= 8 {
						break
					}
					out = append(out, fmt.Sprintf("%s %s", runner, k))
				}
			}
		}
	}
	if has("go.mod") {
		out = append(out, "go build ./...", "go test ./...", "go vet ./...")
	}
	if has("Cargo.toml") {
		out = append(out, "cargo build", "cargo test", "cargo clippy")
	}
	if has("pyproject.toml") {
		if has("uv.lock") {
			out = append(out, "uv sync", "uv run pytest")
		} else {
			out = append(out, "pytest")
		}
	}
	if has("pubspec.yaml") {
		out = append(out, "flutter pub get", "flutter test", "dart analyze")
	}
	if matches, _ := filepath.Glob(filepath.Join(ws, "*.sln")); len(matches) > 0 || hasGlob(ws, "*.csproj") {
		out = append(out, "dotnet build", "dotnet test")
	}
	if has("Makefile") {
		out = append(out, "make")
	}
	return out
}

func hasGlob(ws, pattern string) bool {
	m, _ := filepath.Glob(filepath.Join(ws, pattern))
	return len(m) > 0
}

func applyInstructions(ws string, reg *registry.Registry, profile *registry.Profile, o Options) ([]Action, []string, error) {
	var acts []Action
	var warns []string
	mode := orDefault(o.AgentsMode, ModeCreate)
	agentsPath := filepath.Join(ws, "AGENTS.md")
	_, statErr := os.Stat(agentsPath)
	agentsExists := statErr == nil
	switch {
	case mode == ModeSkip:
	case agentsExists && mode != ModeReplace:
		acts = append(acts, Action{Path: "AGENTS.md", Action: "kept"})
	default:
		content, err := RenderAgentsMD(ws, reg, profile, o.Agents)
		if err != nil {
			return acts, warns, err
		}
		if err := jsonx.WriteFileAtomic(agentsPath, []byte(content), 0o644); err != nil {
			return acts, warns, err
		}
		action := "created"
		if agentsExists {
			action = "replaced"
		}
		acts = append(acts, Action{Path: "AGENTS.md", Action: action})
		agentsExists = true
	}
	for _, id := range o.Harnesses {
		h, _ := harness.Get(id)
		if h.Instructions == "" || !agentsExists {
			continue
		}
		shim := filepath.Join(ws, h.Instructions)
		data, err := os.ReadFile(shim)
		switch {
		case os.IsNotExist(err):
			if err := jsonx.WriteFileAtomic(shim, []byte(h.Import), 0o644); err != nil {
				return acts, warns, err
			}
			acts = append(acts, Action{Path: h.Instructions, Action: "created", Note: "imports AGENTS.md"})
		case err != nil:
			return acts, warns, err
		case !strings.Contains(string(data), "@AGENTS.md") && !strings.Contains(string(data), "@./AGENTS.md"):
			warns = append(warns, fmt.Sprintf("%s exists without an `@AGENTS.md` import; %s will not read AGENTS.md. Add the import line if you want shared instructions.", h.Instructions, h.DisplayName))
		}
	}
	return acts, warns, nil
}

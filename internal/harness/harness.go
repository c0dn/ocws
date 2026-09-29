// Package harness describes the agent harnesses ocws can target and renders
// harness-specific files from shared template sources.
package harness

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

type Harness struct {
	ID          string
	DisplayName string
	// Tokens expand `{name}` placeholders in pack destinations.
	Tokens map[string]string
	// Markers indicate the harness is already in use in a workspace.
	Markers []string
	// Notes are printed after installing anything for this harness.
	Notes []string
}

var All = []Harness{
	{
		ID:          "opencode",
		DisplayName: "OpenCode",
		Tokens: map[string]string{
			"dir": ".opencode", "agents": ".opencode/agents", "commands": ".opencode/commands",
			"skills": ".opencode/skills", "tools": ".opencode/tools", "config": "opencode.json", "mcp": "opencode.json",
		},
		Markers: []string{"opencode.json", "opencode.jsonc", ".opencode"},
	},
	{
		ID:          "claude",
		DisplayName: "Claude Code",
		Tokens: map[string]string{
			"dir": ".claude", "agents": ".claude/agents", "commands": ".claude/commands", "skills": ".claude/skills",
			"config": ".claude/settings.json", "settings": ".claude/settings.json", "mcp": ".mcp.json",
		},
		Markers: []string{"CLAUDE.md", ".claude", ".mcp.json"},
		Notes: []string{
			"Claude Code asks you to approve project MCP servers from .mcp.json on first use.",
			"CLAUDE.md imports AGENTS.md (`@AGENTS.md`) so both harnesses share one instruction file.",
		},
	},
	{
		ID:          "codex",
		DisplayName: "Codex CLI",
		Tokens: map[string]string{
			"dir": ".codex", "agents": ".codex/agents", "skills": ".agents/skills",
			"config": ".codex/config.toml", "mcp": ".codex/config.toml",
		},
		Markers: []string{".codex", ".agents/skills", "AGENTS.override.md"},
		Notes: []string{
			"Codex ignores .codex/config.toml and .codex/agents/ until you trust this project in Codex.",
		},
	},
}

func Get(id string) (Harness, bool) {
	for _, h := range All {
		if h.ID == id {
			return h, true
		}
	}
	return Harness{}, false
}

func IDs() []string {
	out := make([]string, len(All))
	for i, h := range All {
		out[i] = h.ID
	}
	return out
}

// ParseList validates a comma/space separated harness list.
func ParseList(values []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
			if _, ok := Get(part); !ok {
				return nil, fmt.Errorf("unknown harness %q (known: %s)", part, strings.Join(IDs(), ", "))
			}
			if !seen[part] {
				seen[part] = true
				out = append(out, part)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return order(out[i]) < order(out[j]) })
	return out, nil
}

func order(id string) int {
	for i, h := range All {
		if h.ID == id {
			return i
		}
	}
	return len(All)
}

var tokenRe = regexp.MustCompile(`\{([a-z]+)\}`)

// Expand substitutes `{token}` placeholders for a harness.
func (h Harness) Expand(dest string) (string, error) {
	var missing string
	out := tokenRe.ReplaceAllStringFunc(dest, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := h.Tokens[name]
		if !ok {
			missing = name
			return m
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("harness %s has no {%s} location", h.ID, missing)
	}
	return out, nil
}

// Render modes for pack files.
const (
	RenderCopy        = ""
	RenderFrontmatter = "frontmatter"
	RenderCodexAgent  = "codex-agent"
)

// SplitFrontmatter separates a leading `---` YAML block from a markdown body.
func SplitFrontmatter(data []byte) (front []byte, body []byte) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return nil, data
	}
	rest := s[strings.Index(s, "\n")+1:]
	for i := 0; i < len(rest); {
		j := strings.Index(rest[i:], "\n")
		line := rest[i:]
		if j >= 0 {
			line = rest[i : i+j]
		}
		if strings.TrimRight(line, "\r") == "---" {
			end := len(rest)
			if j >= 0 {
				end = i + j + 1
			}
			return []byte(rest[:i]), []byte(strings.TrimLeft(rest[end:], "\r\n"))
		}
		if j < 0 {
			break
		}
		i += j + 1
	}
	return nil, data
}

// Render builds harness-specific file content from a body source (whose own
// frontmatter is discarded) and a header (YAML or TOML).
func Render(mode string, body, header []byte) ([]byte, error) {
	_, content := SplitFrontmatter(body)
	switch mode {
	case RenderFrontmatter:
		var probe map[string]any
		if err := yaml.Unmarshal(header, &probe); err != nil {
			return nil, fmt.Errorf("header is not valid YAML: %w", err)
		}
		var b bytes.Buffer
		b.WriteString("---\n")
		b.Write(bytes.TrimRight(header, "\n"))
		b.WriteString("\n---\n\n")
		b.Write(content)
		return b.Bytes(), nil
	case RenderCodexAgent:
		var probe map[string]any
		if err := toml.Unmarshal(header, &probe); err != nil {
			return nil, fmt.Errorf("header is not valid TOML: %w", err)
		}
		if _, ok := probe["developer_instructions"]; ok {
			return nil, fmt.Errorf("codex-agent header must not set developer_instructions; it comes from the body")
		}
		var b bytes.Buffer
		b.Write(bytes.TrimRight(header, "\n"))
		b.WriteString("\n\ndeveloper_instructions = ")
		b.WriteString(TOMLMultiline(string(content)))
		b.WriteString("\n")
		var check map[string]any
		if err := toml.Unmarshal(b.Bytes(), &check); err != nil {
			return nil, fmt.Errorf("rendered codex agent is not valid TOML: %w", err)
		}
		return b.Bytes(), nil
	}
	return nil, fmt.Errorf("unknown render mode %q", mode)
}

// TOMLMultiline encodes s as a TOML multi-line string, preferring a literal
// string so markdown backslashes survive untouched.
func TOMLMultiline(s string) string {
	if !strings.Contains(s, "'''") && !strings.HasSuffix(s, "'") {
		return "'''\n" + s + "'''"
	}
	r := strings.NewReplacer(`\`, `\\`, `"""`, `""\"`)
	s = r.Replace(s)
	if strings.HasSuffix(s, `"`) {
		s += "\\\n"
	}
	return "\"\"\"\n" + s + "\"\"\""
}

// Package harness describes the agent harnesses ocws can target and renders
// harness-specific files from shared template sources.
package harness

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
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

	// How OpenCode pack files are derived for this harness when a pack has
	// no explicit targets.<id> (see derive.go). Empty = unsupported.
	Agent    string   // AgentMarkdown, AgentCodex, AgentKiro, AgentOpenCodeV1
	AgentExt string   // agent file suffix (default ".md")
	AgentFM  []string // extra agent frontmatter lines, e.g. "model: inherit"
	Command  string   // CommandSkill, CommandMarkdown, CommandTOML, CommandOpenCodeV1
	SkillFM  []string // extra frontmatter for command-skills
	// Args replaces $ARGUMENTS in derived commands (e.g. "{{args}}").
	Args string
	MCP  *MCPStyle
	// MCPFormat overrides the JSON style: "codex" (TOML) or "opencode-v1".
	MCPFormat string
	// Tools: OpenCode custom .ts tools install as-is.
	Tools bool
	// Instructions is a shim file created next to AGENTS.md that imports it
	// (Import is its body); empty when the harness reads AGENTS.md itself.
	Instructions, Import string
	// Excludes lists harnesses that cannot be selected together with this one.
	Excludes []string
	// SkipAgentsWith: skip derived agents when one of these is also selected
	// (the harness already loads their agent directories).
	SkipAgentsWith []string
	// ToolShims: register OpenCode V1 custom tools through generated V2
	// plugins (OpenCode V2 no longer loads .opencode/tools).
	ToolShims bool
	// VersionProbe distinguishes harnesses sharing markers (OpenCode V1/V2).
	VersionProbe string
}

// Derivation formats.
const (
	AgentMarkdown     = "md"
	AgentCodex        = "codex"
	AgentOpenCodeV1   = "opencode-v1"
	CommandSkill      = "skill"
	CommandMarkdown   = "md"
	CommandTOML       = "toml"
	CommandOpenCodeV1 = "opencode-v1"
)

// claudeMCP is shared by Claude Code and Copilot CLI (both read .mcp.json),
// so both must render identical entries.
var claudeMCP = &MCPStyle{Key: "mcpServers", LocalType: "stdio", RemoteType: "http", URLKey: "url", EnvFormat: "${%s}"}

func tokens(dir string, extra map[string]string) map[string]string {
	t := map[string]string{"dir": dir}
	for k, v := range extra {
		t[k] = v
	}
	return t
}

var All = []Harness{
	{
		ID:          "opencode",
		DisplayName: "OpenCode (V2)",
		Tokens: map[string]string{
			"dir": ".opencode", "agents": ".opencode/agents", "commands": ".opencode/commands",
			"skills": ".opencode/skills", "tools": ".opencode/tools", "config": "opencode.json", "mcp": "opencode.json",
		},
		Markers:      []string{"opencode.json", "opencode.jsonc", ".opencode"},
		Excludes:     []string{"opencode-v1"},
		ToolShims:    true,
		VersionProbe: "2",
	},
	{
		ID:          "opencode-v1",
		DisplayName: "OpenCode V1",
		Tokens: map[string]string{
			"dir": ".opencode", "agents": ".opencode/agents", "commands": ".opencode/commands",
			"skills": ".opencode/skills", "tools": ".opencode/tools", "config": "opencode.json", "mcp": "opencode.json",
		},
		Markers:      []string{"opencode.json", "opencode.jsonc", ".opencode"},
		Agent:        AgentOpenCodeV1,
		Command:      CommandOpenCodeV1,
		MCPFormat:    "opencode-v1",
		Tools:        true,
		Excludes:     []string{"opencode"},
		VersionProbe: "1",
		Notes:        []string{"OpenCode V1 files are lowered from the V2 templates (permissions list -> permission map, mcp.servers -> mcp, model#variant -> model + variant). OpenCode V2 also reads this V1 shape."},
	},
	{
		ID:          "claude",
		DisplayName: "Claude Code",
		Tokens: map[string]string{
			"dir": ".claude", "agents": ".claude/agents", "commands": ".claude/commands", "skills": ".claude/skills",
			"config": ".claude/settings.json", "settings": ".claude/settings.json", "mcp": ".mcp.json",
		},
		Markers:      []string{"CLAUDE.md", ".claude", ".mcp.json"},
		Agent:        AgentMarkdown,
		AgentFM:      []string{"model: inherit"},
		Command:      CommandSkill,
		SkillFM:      []string{"disable-model-invocation: true"},
		MCP:          claudeMCP,
		Instructions: "CLAUDE.md", Import: "# Claude Code instructions\n\nShared project instructions live in AGENTS.md.\n\n@AGENTS.md\n",
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
		Markers:   []string{".codex", "AGENTS.override.md"},
		Agent:     AgentCodex,
		AgentExt:  ".toml",
		Command:   CommandSkill,
		MCPFormat: "codex",
		Notes: []string{
			"Codex ignores .codex/config.toml and .codex/agents/ until you trust this project in Codex.",
		},
	},
	{
		ID: "gemini", DisplayName: "Gemini CLI",
		Tokens: tokens(".gemini", map[string]string{"agents": ".gemini/agents", "commands": ".gemini/commands", "skills": ".agents/skills",
			"config": ".gemini/settings.json", "mcp": ".gemini/settings.json"}),
		Markers: []string{"GEMINI.md", ".gemini"},
		Agent:   AgentMarkdown, Command: CommandTOML, Args: "{{args}}",
		MCP:          &MCPStyle{Key: "mcpServers", URLKey: "httpUrl", EnvFormat: "${%s}"},
		Instructions: "GEMINI.md", Import: "# Gemini CLI instructions\n\nShared project instructions live in AGENTS.md.\n\n@./AGENTS.md\n",
		Notes: []string{"Gemini CLI skips project settings, MCP servers and custom commands until you trust this folder (or set GEMINI_CLI_TRUST_WORKSPACE=true)."},
	},
	{
		ID: "qwen", DisplayName: "Qwen Code",
		Tokens: tokens(".qwen", map[string]string{"agents": ".qwen/agents", "commands": ".qwen/commands", "skills": ".qwen/skills",
			"config": ".qwen/settings.json", "mcp": ".qwen/settings.json"}),
		Markers: []string{"QWEN.md", ".qwen"},
		Agent:   AgentMarkdown, Command: CommandMarkdown, Args: "{{args}}",
		MCP: &MCPStyle{Key: "mcpServers", URLKey: "httpUrl", EnvFormat: "${%s}"},
	},
	{
		ID: "copilot", DisplayName: "GitHub Copilot CLI",
		Tokens:  tokens(".github", map[string]string{"agents": ".github/agents", "skills": ".agents/skills", "mcp": ".mcp.json"}),
		Markers: []string{".github/agents", ".github/copilot-instructions.md", ".github/skills"},
		Agent:   AgentMarkdown, AgentExt: ".agent.md", Command: CommandSkill,
		MCP:   claudeMCP,
		Notes: []string{"Copilot CLI asks whether you trust this folder before loading its agents, skills and MCP servers."},
	},
	{
		ID: "cursor", DisplayName: "Cursor CLI",
		Tokens:  tokens(".cursor", map[string]string{"agents": ".cursor/agents", "skills": ".agents/skills", "mcp": ".cursor/mcp.json"}),
		Markers: []string{".cursor"},
		Agent:   AgentMarkdown, AgentFM: []string{"model: inherit"}, Command: CommandSkill,
		MCP:            &MCPStyle{Key: "mcpServers", LocalType: "stdio", URLKey: "url", EnvFormat: "${env:%s}"},
		SkipAgentsWith: []string{"claude", "codex"},
		Notes:          []string{"Cursor CLI needs project MCP servers approved (`agent mcp enable <name>` or --approve-mcps)."},
	},
	{
		ID: "droid", DisplayName: "Factory Droid",
		Tokens:  tokens(".factory", map[string]string{"agents": ".factory/droids", "commands": ".factory/commands", "skills": ".agents/skills", "mcp": ".factory/mcp.json"}),
		Markers: []string{".factory"},
		Agent:   AgentMarkdown, AgentFM: []string{"model: inherit"}, Command: CommandMarkdown,
		MCP: &MCPStyle{Key: "mcpServers", LocalType: "stdio", RemoteType: "http", URLKey: "url", EnvFormat: "${%s}", DisabledKey: "disabled"},
	},
	{
		ID: "kiro", DisplayName: "Kiro CLI",
		Tokens:  tokens(".kiro", map[string]string{"agents": ".kiro/agents", "commands": ".kiro/prompts", "skills": ".kiro/skills", "mcp": ".kiro/settings/mcp.json"}),
		Markers: []string{".kiro"},
		Agent:   AgentMarkdown, AgentFM: []string{"tools: [\"*\"]"}, Command: CommandMarkdown,
		MCP:   &MCPStyle{Key: "mcpServers", URLKey: "url", EnvFormat: "${%s}", DisabledKey: "disabled"},
		Notes: []string{"Kiro loads workspace agents only after you trust the workspace."},
	},
	{
		ID: "amp", DisplayName: "Amp",
		Tokens:  tokens(".amp", map[string]string{"skills": ".agents/skills", "config": ".amp/settings.json", "mcp": ".amp/settings.json"}),
		Markers: []string{".amp"},
		Command: CommandSkill,
		MCP:     &MCPStyle{Key: "amp.mcpServers", URLKey: "url", EnvFormat: "${%s}"},
		Notes:   []string{"Amp needs workspace MCP servers approved: `amp mcp approve <name>`."},
	},
	{
		ID: "crush", DisplayName: "Crush",
		Tokens:    tokens(".crush", map[string]string{"commands": ".crush/commands", "skills": ".agents/skills", "mcp": ".crushrc"}),
		Markers:   []string{".crush", ".crushrc", "crushrc", ".crush.json", "crush.json", "CRUSH.md"},
		Command:   CommandMarkdown,
		MCPFormat: "crushrc",
		Notes:     []string{"Crush runs .crushrc as trusted Bash at startup; ocws appends `mcp add` lines to it and removes exactly those lines on uninstall."},
	},
	{
		ID: "goose", DisplayName: "Goose",
		Tokens:  tokens(".goose", map[string]string{"agents": ".agents/agents", "skills": ".agents/skills"}),
		Markers: []string{".goosehints", ".goose"},
		Agent:   AgentMarkdown, Command: CommandSkill,
		Notes: []string{"Goose keeps MCP extensions in its global config.yaml; ocws only installs project-local agents and skills."},
	},
	{
		ID: "cline", DisplayName: "Cline CLI",
		Tokens:  tokens(".cline", map[string]string{"skills": ".agents/skills"}),
		Markers: []string{".clinerules", ".cline"},
		Command: CommandSkill,
		Notes:   []string{"Cline keeps MCP servers in its global settings; ocws only installs project-local skills."},
	},
	{
		ID: "kilo", DisplayName: "Kilo Code CLI",
		Tokens: tokens(".kilo", map[string]string{"agents": ".kilo/agents", "commands": ".kilo/commands", "skills": ".kilo/skills",
			"config": "kilo.json", "mcp": "kilo.json"}),
		Markers: []string{"kilo.json", "kilo.jsonc", ".kilo", ".kilocode"},
		Agent:   AgentOpenCodeV1, Command: CommandOpenCodeV1, MCPFormat: "opencode-v1",
		Notes: []string{"Kilo Code is an OpenCode V1 fork; its files are lowered from the V2 templates into .kilo/ and kilo.json."},
	},
	{
		ID: "pi", DisplayName: "pi",
		Tokens:  tokens(".pi", map[string]string{"commands": ".pi/prompts", "skills": ".agents/skills", "mcp": ".pi/mcp.json"}),
		Markers: []string{".pi"},
		Command: CommandMarkdown,
		MCP:     &MCPStyle{Key: "mcpServers", RemoteType: "http", URLKey: "url", EnvFormat: "${%s}"},
		Notes:   []string{"pi loads project .pi/ files and .agents/skills only after you trust the project (`pi -a`, or accept the prompt)."},
	},
	{
		ID: "hermes", DisplayName: "Hermes Agent",
		Tokens:  tokens(".hermes", map[string]string{"skills": ".agents/skills"}),
		Markers: []string{".hermes", ".hermes.md", "HERMES.md"},
		Command: CommandSkill,
		Notes:   []string{"Hermes loads project skills only after `hermes skills trust`; its MCP servers live in the global ~/.hermes/config.yaml, which ocws does not edit."},
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

// CheckExclusive rejects harness combinations that share files, e.g.
// OpenCode V1 and V2.
func CheckExclusive(ids []string) error {
	for _, id := range ids {
		h, _ := Get(id)
		for _, x := range h.Excludes {
			if slices.Contains(ids, x) {
				return fmt.Errorf("harnesses %s and %s write the same files; select only one", id, x)
			}
		}
	}
	return nil
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

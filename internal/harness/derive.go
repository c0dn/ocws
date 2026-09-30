package harness

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/c0dn/ocws/internal/model"
	"go.yaml.in/yaml/v3"
)

// Derived render modes. The engine renders these from the OpenCode source
// alone (no header file), so packs need no per-harness targets.
const (
	RenderOpenCodeV1  = "opencode-v1"
	renderAgentPrefix = "agent:"
	renderCmdPrefix   = "command:"
	renderMCPPrefix   = "mcp:"
	RenderSkillPolicy = "codex-skill-policy"
)

// Derivation is the result of deriving one pack for one harness.
type Derivation struct {
	Files    []model.FilePlan
	Warnings []string
	// Skip is set when nothing in the pack can be installed for the harness.
	Skip string
}

// Derive maps a pack's OpenCode files (destinations already expanded for
// OpenCode) to harness h. dir is the pack directory, used to read MCP
// fragments so their JSON pointers are known at plan time. selected lists
// every harness in the plan.
func (h Harness) Derive(files []model.FilePlan, dir string, selected []string) Derivation {
	var d Derivation
	warn := func(format string, a ...any) { d.Warnings = append(d.Warnings, fmt.Sprintf(format, a...)) }
	for _, f := range files {
		if (strings.HasPrefix(f.Destination, ".opencode/tools/") || f.Role == "tool") && !h.Tools {
			d.Skip = fmt.Sprintf("pack ships OpenCode custom tools (%s); %s has no equivalent", f.Destination, h.DisplayName)
			return d
		}
	}
	skipAgents := ""
	for _, other := range h.SkipAgentsWith {
		if slices.Contains(selected, other) {
			skipAgents = other
			break
		}
	}
	add := func(f model.FilePlan) { d.Files = append(d.Files, f) }
	for _, f := range files {
		dest := f.Destination
		out := f
		switch {
		case h.Agent == AgentOpenCodeV1:
			// Same layout (relocated for forks such as Kilo); only
			// frontmatter and config shapes change.
			if isOpenCodeConfig(dest) {
				out.Destination = h.Tokens["config"]
			} else if strings.HasPrefix(dest, ".opencode/") {
				out.Destination = h.Tokens["dir"] + "/" + strings.TrimPrefix(dest, ".opencode/")
			}
			switch {
			case f.InstallMode == "merge" && isOpenCodeConfig(dest):
				out.Render = RenderOpenCodeV1
				out.Header = ""
				var ptrs []string
				for _, p := range f.JSONPointers {
					ptrs = append(ptrs, LowerPointer(p))
				}
				out.JSONPointers = ptrs
			case strings.HasSuffix(dest, ".md") && (strings.HasPrefix(dest, ".opencode/agents/") || strings.HasPrefix(dest, ".opencode/commands/")) && f.Render == "":
				out.Render = RenderOpenCodeV1
			}
			add(out)

		case strings.HasPrefix(dest, ".opencode/agents/") && strings.HasSuffix(dest, ".md"):
			name := strings.TrimSuffix(path.Base(dest), ".md")
			switch {
			case h.Agent == "" || h.Tokens["agents"] == "":
				warn("agent %s skipped: %s has no project-level custom agents", name, h.DisplayName)
			case skipAgents != "":
				warn("agent %s skipped: %s already loads %s agents", name, h.DisplayName, skipAgents)
			default:
				if front := readFrontmatter(filepath.Join(dir, f.Source)); front["mode"] == "primary" {
					warn("agent %s is an OpenCode primary agent; %s installs it as a subagent", name, h.DisplayName)
				}
				out.Destination = h.Tokens["agents"] + "/" + name + orStr(h.AgentExt, ".md")
				out.Render, out.Header = renderAgentPrefix+h.ID, ""
				add(out)
			}

		case strings.HasPrefix(dest, ".opencode/commands/") && strings.HasSuffix(dest, ".md"):
			name := strings.TrimSuffix(path.Base(dest), ".md")
			out.Render, out.Header = renderCmdPrefix+h.ID, ""
			switch {
			case h.Command == CommandSkill && h.Tokens["skills"] != "":
				out.Destination = h.Tokens["skills"] + "/" + name + "/SKILL.md"
				add(out)
				if h.Agent == AgentCodex {
					add(model.FilePlan{Source: f.Source, Destination: h.Tokens["skills"] + "/" + name + "/agents/openai.yaml",
						Managed: f.Managed, Role: "support", Render: RenderSkillPolicy})
				}
			case (h.Command == CommandMarkdown || h.Command == CommandTOML) && h.Tokens["commands"] != "":
				ext := ".md"
				if h.Command == CommandTOML {
					ext = ".toml"
				}
				out.Destination = h.Tokens["commands"] + "/" + name + ext
				add(out)
			default:
				warn("command %s skipped: %s has no project-level custom commands", name, h.DisplayName)
			}

		case strings.HasPrefix(dest, ".opencode/skills/"):
			if h.Tokens["skills"] == "" {
				warn("skill %s skipped: %s has no project-level skills directory", strings.TrimPrefix(dest, ".opencode/skills/"), h.DisplayName)
				continue
			}
			out.Destination = h.Tokens["skills"] + "/" + strings.TrimPrefix(dest, ".opencode/skills/")
			add(out)

		case f.InstallMode == "merge" && isOpenCodeConfig(dest):
			var nonMCP []string
			for _, p := range f.JSONPointers {
				if !strings.HasPrefix(p, "/mcp/") {
					nonMCP = append(nonMCP, p)
				}
			}
			if len(nonMCP) > 0 {
				warn("%s: %s is OpenCode-specific config; not applied to %s", f.Source, strings.Join(nonMCP, ", "), h.DisplayName)
			}
			if len(nonMCP) == len(f.JSONPointers) && len(f.JSONPointers) > 0 {
				continue
			}
			if h.MCP == nil && h.MCPFormat != "codex" && h.MCPFormat != "crushrc" {
				warn("%s: MCP servers skipped: %s has no project-level MCP config", f.Source, h.DisplayName)
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, f.Source))
			if err != nil {
				warn("%s: %v", f.Source, err)
				continue
			}
			out.Destination = h.Tokens["mcp"]
			out.Render, out.Header = renderMCPPrefix+h.ID, ""
			if h.MCPFormat == "codex" {
				out.InstallMode, out.JSONPointers = "toml-merge", nil
			} else if h.MCPFormat == "crushrc" {
				out.InstallMode, out.JSONPointers = "text-merge", nil
			} else {
				_, ptrs, w, err := h.MCP.JSONFragment(data)
				if err != nil {
					warn("%s: %v", f.Source, err)
					continue
				}
				d.Warnings = append(d.Warnings, w...)
				if len(ptrs) == 0 {
					continue
				}
				out.JSONPointers = ptrs
			}
			out.Role = "merged-fragment"
			add(out)

		case strings.HasPrefix(dest, ".opencode/"):
			if h.Tokens["dir"] == "" {
				warn("%s skipped: %s has no config directory", dest, h.DisplayName)
				continue
			}
			out.Destination = h.Tokens["dir"] + "/" + strings.TrimPrefix(dest, ".opencode/")
			add(out)

		case f.InstallMode != "" && f.InstallMode != "copy":
			warn("%s (installMode=%s) has no %s translation; skipped", dest, f.InstallMode, h.DisplayName)

		default:
			add(out)
		}
	}
	if len(d.Files) == 0 && d.Skip == "" {
		d.Skip = "nothing in the pack applies to " + h.DisplayName
		if len(d.Warnings) > 0 {
			d.Skip += " (" + strings.Join(d.Warnings, "; ") + ")"
			d.Warnings = nil
		}
	}
	return d
}

func isOpenCodeConfig(dest string) bool { return dest == "opencode.json" || dest == "opencode.jsonc" }

func readFrontmatter(p string) map[string]any {
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{}
	}
	return frontmatterMap(data)
}

func frontmatterMap(data []byte) map[string]any {
	front, _ := SplitFrontmatter(data)
	m := map[string]any{}
	yaml.Unmarshal(front, &m)
	return m
}

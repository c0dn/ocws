package harness

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

// RenderFile renders a pack file for its destination. Header-based modes
// (frontmatter, codex-agent) need header; derived modes build everything
// from the OpenCode source body.
func RenderFile(mode string, body, header []byte, dest string) ([]byte, error) {
	switch {
	case mode == RenderOpenCodeV1:
		if strings.HasSuffix(dest, ".json") || strings.HasSuffix(dest, ".jsonc") {
			return LowerConfig(body)
		}
		return LowerMarkdown(body)
	case mode == RenderSkillPolicy:
		name := path.Base(path.Dir(path.Dir(dest)))
		return []byte("# Command-style skill: run only when invoked explicitly ($" + name + ").\npolicy:\n  allow_implicit_invocation: false\n"), nil
	case strings.HasPrefix(mode, renderAgentPrefix):
		h, err := derivedHarness(mode, renderAgentPrefix)
		if err != nil {
			return nil, err
		}
		return h.renderAgent(body, dest)
	case strings.HasPrefix(mode, renderCmdPrefix):
		h, err := derivedHarness(mode, renderCmdPrefix)
		if err != nil {
			return nil, err
		}
		return h.renderCommand(body, dest)
	case strings.HasPrefix(mode, renderMCPPrefix):
		h, err := derivedHarness(mode, renderMCPPrefix)
		if err != nil {
			return nil, err
		}
		switch {
		case h.MCPFormat == "codex":
			out, _, err := CodexFragment(body, "")
			return out, err
		case h.MCP != nil:
			out, _, _, err := h.MCP.JSONFragment(body)
			return out, err
		}
		return nil, fmt.Errorf("harness %s has no MCP format", h.ID)
	}
	return Render(mode, body, header)
}

// IsDerived reports whether a render mode needs no header file.
func IsDerived(mode string) bool {
	return mode == RenderOpenCodeV1 || mode == RenderSkillPolicy || strings.HasPrefix(mode, renderAgentPrefix) ||
		strings.HasPrefix(mode, renderCmdPrefix) || strings.HasPrefix(mode, renderMCPPrefix)
}

func derivedHarness(mode, prefix string) (Harness, error) {
	id := strings.TrimPrefix(mode, prefix)
	h, ok := Get(id)
	if !ok {
		return h, fmt.Errorf("render mode %s names unknown harness %q", mode, id)
	}
	return h, nil
}

func yamlLine(key string, value any) string {
	out, _ := yaml.Marshal(map[string]any{key: value})
	return string(out)
}

func frontmatterDoc(lines string, body []byte) []byte {
	var b bytes.Buffer
	b.WriteString("---\n" + lines + "---\n\n")
	b.Write(body)
	return b.Bytes()
}

func description(front map[string]any) string {
	d, _ := front["description"].(string)
	return strings.TrimSpace(d)
}

func (h Harness) renderAgent(src []byte, dest string) ([]byte, error) {
	front := frontmatterMap(src)
	_, body := SplitFrontmatter(src)
	name := strings.TrimSuffix(path.Base(dest), orStr(h.AgentExt, ".md"))
	desc := description(front)
	if desc == "" {
		desc = name + " agent"
	}
	switch h.Agent {
	case AgentMarkdown:
		lines := yamlLine("name", name) + yamlLine("description", desc)
		for _, l := range h.AgentFM {
			lines += l + "\n"
		}
		return frontmatterDoc(lines, body), nil
	case AgentCodex:
		kv, err := toml.Marshal(struct {
			Name        string `toml:"name"`
			Description string `toml:"description"`
		}{name, desc})
		if err != nil {
			return nil, err
		}
		return Render(RenderCodexAgent, src, kv)
	}
	return nil, fmt.Errorf("harness %s has no agent format", h.ID)
}

func (h Harness) renderCommand(src []byte, dest string) ([]byte, error) {
	front := frontmatterMap(src)
	_, body := SplitFrontmatter(src)
	switch h.Command {
	case CommandSkill:
		name := path.Base(path.Dir(dest))
		desc := description(front)
		if desc == "" {
			desc = "Run the " + name + " command."
		}
		lines := yamlLine("name", name) + yamlLine("description", desc)
		for _, l := range h.SkillFM {
			lines += l + "\n"
		}
		return frontmatterDoc(lines, body), nil
	case CommandMarkdown:
		if h.Args != "" {
			body = []byte(strings.ReplaceAll(string(body), "$ARGUMENTS", h.Args))
		}
		if desc := description(front); desc != "" {
			return frontmatterDoc(yamlLine("description", desc), body), nil
		}
		return body, nil
	case CommandTOML:
		prompt := strings.ReplaceAll(string(body), "$ARGUMENTS", orStr(h.Args, "{{args}}"))
		var b strings.Builder
		if desc := description(front); desc != "" {
			d, _ := toml.Marshal(map[string]string{"description": desc})
			b.Write(d)
		}
		b.WriteString("prompt = " + TOMLMultiline(prompt) + "\n")
		var check map[string]any
		if err := toml.Unmarshal([]byte(b.String()), &check); err != nil {
			return nil, fmt.Errorf("rendered command is not valid TOML: %w", err)
		}
		return []byte(b.String()), nil
	}
	return nil, fmt.Errorf("harness %s has no command format", h.ID)
}

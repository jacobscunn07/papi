package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontmatter splits a markdown file with YAML frontmatter (delimited by ---)
// and returns the parsed data map and the body content.
func ParseFrontmatter(raw string) (map[string]any, string, error) {
	if !strings.HasPrefix(raw, "---") {
		return nil, raw, nil
	}
	// Find the closing ---
	rest := raw[3:]
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return nil, raw, fmt.Errorf("unclosed frontmatter")
	}
	yamlBlock := strings.TrimSpace(rest[:idx])
	body := strings.TrimPrefix(rest[idx+4:], "\n")

	var data map[string]any
	if err := yaml.Unmarshal([]byte(yamlBlock), &data); err != nil {
		return nil, body, fmt.Errorf("parse frontmatter yaml: %w", err)
	}
	return data, body, nil
}

// scalarLineRe matches a top-level "key: value" frontmatter line with an inline value.
var scalarLineRe = regexp.MustCompile(`^([A-Za-z0-9_-]+):[ \t]+(\S.*)$`)

// RepairFrontmatter attempts to fix a SKILL.md whose YAML frontmatter fails to parse
// because a single-line scalar value (typically description) contains YAML-special
// characters like ": " and was left unquoted by the research agent.
//
// It re-quotes plain inline scalar values using YAML-safe double quotes and only
// accepts the result if it turns an invalid block into a valid one — so it can never
// make a good file worse. Returns (fixed, true) on success (including the no-op case
// where the input already parses) and (raw, false) if it can't be repaired.
func RepairFrontmatter(raw string) (string, bool) {
	if _, _, err := ParseFrontmatter(raw); err == nil {
		return raw, true
	}
	if !strings.HasPrefix(raw, "---") {
		return raw, false
	}
	rest := raw[3:]
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return raw, false
	}
	block := rest[:idx]
	suffix := rest[idx:] // "\n---" + body, preserved verbatim

	lines := strings.Split(block, "\n")
	for i, line := range lines {
		m := scalarLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		val := strings.TrimRight(m[2], " \t")
		// Leave values that are already quoted or start a block scalar (| or >) alone.
		if strings.HasPrefix(val, "'") || strings.HasPrefix(val, `"`) ||
			strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">") {
			continue
		}
		escaped := strings.ReplaceAll(val, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		lines[i] = m[1] + `: "` + escaped + `"`
	}

	fixed := "---" + strings.Join(lines, "\n") + suffix
	if _, _, err := ParseFrontmatter(fixed); err != nil {
		return raw, false
	}
	return fixed, true
}

// ReadSkillMd reads SKILL.md and returns description, body content, and raw text.
func ReadSkillMd(skillDir string) (description, content, raw string, err error) {
	rawBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return "", "", "", err
	}
	raw = string(rawBytes)
	data, body, err := ParseFrontmatter(raw)
	if err != nil {
		return "", "", raw, err
	}
	if data != nil {
		if d, ok := data["description"].(string); ok {
			description = d
		}
	}
	return description, body, raw, nil
}

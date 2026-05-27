package config

import (
	"fmt"
	"os"
	"path/filepath"
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

package config

import (
	"fmt"
	"strings"

	"papi/internal/types"
)

// A skill snapshot is stored as a single string (the store column, the TUI diff, the
// resume checkpoint all take one blob). The bundle encoding packs SKILL.md and its
// reference files into that one string, delimited by a marker line:
//
//	<SKILL.md content>
//	<!-- papi:file references/modules.md -->
//	<content>
//	<!-- papi:file references/ci-testing.md -->
//	<content>
//
// It is deliberately a readable markdown comment rather than JSON, so the existing
// iteration diff in the TUI stays legible when reference files change. Round-tripping
// is exact because every section is normalized to end in exactly one newline by
// ValidateProposal before it is ever marshalled.
const (
	bundleSentinelPrefix = "<!-- papi:file "
	bundleSentinelSuffix = " -->"
)

func bundleSentinel(path string) string {
	return bundleSentinelPrefix + path + bundleSentinelSuffix
}

// parseBundleSentinel reports whether a line is a section marker and, if so, the path
// it introduces.
func parseBundleSentinel(line string) (string, bool) {
	trimmed := strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(trimmed, bundleSentinelPrefix) || !strings.HasSuffix(trimmed, bundleSentinelSuffix) {
		return "", false
	}
	path := trimmed[len(bundleSentinelPrefix) : len(trimmed)-len(bundleSentinelSuffix)]
	if path == "" {
		return "", false
	}
	return path, true
}

// containsBundleSentinel reports whether any line of s would be mistaken for a section
// marker, which would let content forge a section boundary when the snapshot is decoded.
func containsBundleSentinel(s string) (string, bool) {
	for _, line := range strings.Split(s, "\n") {
		if _, ok := parseBundleSentinel(line); ok {
			return strings.TrimRight(line, "\r"), true
		}
	}
	return "", false
}

// MarshalBundle packs SKILL.md and its reference files into one snapshot string.
func MarshalBundle(skillMd string, files []types.SkillFile) string {
	if len(files) == 0 {
		return skillMd
	}
	var sb strings.Builder
	sb.WriteString(skillMd)
	if !strings.HasSuffix(skillMd, "\n") {
		sb.WriteString("\n")
	}
	for _, f := range files {
		sb.WriteString(bundleSentinel(f.Path))
		sb.WriteString("\n")
		sb.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// UnmarshalBundle splits a snapshot back into SKILL.md and its reference files. A
// snapshot written before reference files existed has no markers and decodes to just
// SKILL.md, so old runs still load.
func UnmarshalBundle(bundle string) (string, []types.SkillFile, error) {
	var skillMd strings.Builder
	var files []types.SkillFile
	var cur strings.Builder
	curPath := ""

	flush := func() {
		if curPath != "" {
			files = append(files, types.SkillFile{Path: curPath, Content: cur.String()})
			cur.Reset()
		}
	}

	rest := bundle
	for len(rest) > 0 {
		line := rest
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i+1], rest[i+1:]
		} else {
			rest = ""
		}
		if p, ok := parseBundleSentinel(line); ok {
			flush()
			curPath = p
			continue
		}
		if curPath == "" {
			skillMd.WriteString(line)
		} else {
			cur.WriteString(line)
		}
	}
	flush()

	for _, f := range files {
		if reason := validatePath(f.Path); reason != "" {
			return "", nil, fmt.Errorf("bundle has invalid file path %s", reason)
		}
	}
	return skillMd.String(), files, nil
}

package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"papi/internal/types"
)

// ReferencesDir is the only subdirectory of a skill the research agent may write.
// It holds the progressive-disclosure files SKILL.md points at and Claude reads on
// demand, keeping SKILL.md itself a short router.
const ReferencesDir = "references"

// Caps on a single proposal, so a runaway agent cannot blow up the prompt (every
// reference file is fed back to it next iteration) or the repo.
const (
	MaxProposalFiles = 12
	MaxProposalBytes = 256 * 1024
)

// referenceLinkRe finds mentions of a reference file anywhere in a markdown body:
// a markdown link, a bare path, or an inline-code path all match.
var referenceLinkRe = regexp.MustCompile(`references/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*\.md`)

// normalizeContent makes a file body canonical: LF line endings and exactly one
// trailing newline. Doing this once up front is what makes the bundle encoding in
// bundle.go an exact round-trip, and it is what we want on disk anyway.
func normalizeContent(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

// validatePath reports why a proposed file path is unacceptable, or "" if it is fine.
// Paths are relative to the skill dir and confined to references/*.md so a proposal
// can never reach SKILL.md, README.md, or anything outside the skill.
func validatePath(p string) string {
	if p == "" {
		return "empty path"
	}
	slashed := filepath.ToSlash(p)
	if slashed != p {
		return fmt.Sprintf("%q: must use forward slashes", p)
	}
	if path.IsAbs(slashed) || strings.HasPrefix(slashed, "/") {
		return fmt.Sprintf("%q: must be relative to the skill directory", p)
	}
	if path.Clean(slashed) != slashed {
		return fmt.Sprintf("%q: must be a clean path (no ./, //, or trailing /)", p)
	}
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".." {
			return fmt.Sprintf("%q: must not escape the skill directory", p)
		}
	}
	if !strings.HasPrefix(slashed, ReferencesDir+"/") {
		return fmt.Sprintf("%q: supporting files must live under %s/", p, ReferencesDir)
	}
	if !strings.HasSuffix(slashed, ".md") {
		return fmt.Sprintf("%q: supporting files must be markdown (.md)", p)
	}
	return ""
}

// linkedReferences returns the set of references/*.md paths mentioned in a body.
func linkedReferences(body string) map[string]bool {
	found := map[string]bool{}
	for _, m := range referenceLinkRe.FindAllString(body, -1) {
		found[m] = true
	}
	return found
}

// ValidateProposal checks and canonicalizes a research-agent proposal: SKILL.md plus
// the reference files it discloses progressively. It returns the repaired SKILL.md and
// normalized files, or an error explaining why the proposal must be rejected.
//
// The important check is link integrity in both directions. A SKILL.md that points at a
// reference file the agent never supplied would silently gut quality - Claude follows the
// pointer, finds nothing, and answers from a router with no content behind it - which is
// strictly worse than one long SKILL.md. Orphan files are rejected for the mirror reason:
// content nothing routes to is dead weight in the repo.
func ValidateProposal(skillMd string, files []types.SkillFile) (string, []types.SkillFile, error) {
	if strings.TrimSpace(skillMd) == "" {
		return "", nil, fmt.Errorf("proposal has an empty SKILL.md")
	}

	fixed, ok := RepairFrontmatter(normalizeContent(skillMd))
	if !ok {
		return "", nil, fmt.Errorf("SKILL.md has invalid YAML frontmatter that could not be repaired")
	}

	if len(files) > MaxProposalFiles {
		return "", nil, fmt.Errorf("proposal has %d supporting files, limit is %d", len(files), MaxProposalFiles)
	}

	total := len(fixed)
	seen := make(map[string]bool, len(files))
	normalized := make([]types.SkillFile, 0, len(files))
	for _, f := range files {
		if reason := validatePath(f.Path); reason != "" {
			return "", nil, fmt.Errorf("invalid supporting file path %s", reason)
		}
		if seen[f.Path] {
			return "", nil, fmt.Errorf("duplicate supporting file %q", f.Path)
		}
		seen[f.Path] = true
		content := normalizeContent(f.Content)
		if strings.TrimSpace(content) == "" {
			return "", nil, fmt.Errorf("supporting file %q is empty", f.Path)
		}
		total += len(content)
		normalized = append(normalized, types.SkillFile{Path: f.Path, Content: content})
	}
	if total > MaxProposalBytes {
		return "", nil, fmt.Errorf("proposal is %d bytes, limit is %d", total, MaxProposalBytes)
	}

	// The bundle encoding used for run snapshots is line-delimited; content carrying
	// its sentinel would let a proposal forge section boundaries on resume.
	if line, bad := containsBundleSentinel(fixed); bad {
		return "", nil, fmt.Errorf("SKILL.md contains the reserved bundle marker %q", line)
	}
	for _, f := range normalized {
		if line, bad := containsBundleSentinel(f.Content); bad {
			return "", nil, fmt.Errorf("supporting file %q contains the reserved bundle marker %q", f.Path, line)
		}
	}

	// Link integrity. A file counts as reachable if SKILL.md links it or another
	// supplied reference does, so a reference may itself disclose a deeper one.
	mentioned := linkedReferences(fixed)
	for _, f := range normalized {
		for p := range linkedReferences(f.Content) {
			mentioned[p] = true
		}
	}

	var dangling []string
	for p := range mentioned {
		if !seen[p] {
			dangling = append(dangling, p)
		}
	}
	if len(dangling) > 0 {
		sort.Strings(dangling)
		return "", nil, fmt.Errorf("SKILL.md links reference file(s) the proposal never supplies: %s", strings.Join(dangling, ", "))
	}

	var orphans []string
	for _, f := range normalized {
		if !mentioned[f.Path] {
			orphans = append(orphans, f.Path)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return "", nil, fmt.Errorf("supporting file(s) not linked from SKILL.md or any other reference: %s", strings.Join(orphans, ", "))
	}

	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Path < normalized[j].Path })
	return fixed, normalized, nil
}

// ReadSkillFiles returns the skill's supporting reference files from disk, sorted by
// path. A missing references/ directory is not an error - it just means the skill has
// not been split up yet.
func ReadSkillFiles(skillDir string) ([]types.SkillFile, error) {
	root := filepath.Join(skillDir, ReferencesDir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var files []types.SkillFile
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(skillDir, p)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, types.SkillFile{Path: filepath.ToSlash(rel), Content: string(raw)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// WriteSkillFiles replaces the skill's SKILL.md and references/ tree with the given
// proposal. references/ is removed first so a proposal that drops a reference file
// actually shrinks the skill instead of leaving the old file behind for Claude to find.
func WriteSkillFiles(skillDir, skillMd string, files []types.SkillFile) error {
	if err := os.RemoveAll(filepath.Join(skillDir, ReferencesDir)); err != nil {
		return fmt.Errorf("clear %s/: %w", ReferencesDir, err)
	}
	for _, f := range files {
		dest := filepath.Join(skillDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte(f.Content), 0644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMd), 0644)
}

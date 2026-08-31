package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"papi/internal/types"
)

const validSkillMd = `---
name: probe
description: A probe skill.
---

# Probe

See references/depth.md for the details.
`

func file(path, content string) types.SkillFile {
	return types.SkillFile{Path: path, Content: content}
}

func TestValidateProposal_AcceptsLinkedReference(t *testing.T) {
	md, files, err := ValidateProposal(validSkillMd, []types.SkillFile{file("references/depth.md", "# Depth\n\nbody")})
	if err != nil {
		t.Fatalf("expected valid proposal, got %v", err)
	}
	if len(files) != 1 || files[0].Path != "references/depth.md" {
		t.Fatalf("unexpected files: %+v", files)
	}
	if !strings.HasSuffix(files[0].Content, "body\n") {
		t.Errorf("content should be normalized to one trailing newline, got %q", files[0].Content)
	}
	if !strings.Contains(md, "name: probe") {
		t.Errorf("SKILL.md mangled: %q", md)
	}
}

func TestValidateProposal_AcceptsNoFiles(t *testing.T) {
	plain := "---\nname: probe\ndescription: A probe skill.\n---\n\n# Probe\n"
	if _, files, err := ValidateProposal(plain, nil); err != nil || len(files) != 0 {
		t.Fatalf("a single-file proposal must stay valid: files=%v err=%v", files, err)
	}
}

func TestValidateProposal_RejectsDanglingLink(t *testing.T) {
	_, _, err := ValidateProposal(validSkillMd, nil)
	if err == nil || !strings.Contains(err.Error(), "never supplies") {
		t.Fatalf("expected dangling-link rejection, got %v", err)
	}
}

func TestValidateProposal_RejectsOrphanFile(t *testing.T) {
	_, _, err := ValidateProposal(validSkillMd, []types.SkillFile{
		file("references/depth.md", "linked"),
		file("references/nobody-links-me.md", "orphan"),
	})
	if err == nil || !strings.Contains(err.Error(), "not linked") {
		t.Fatalf("expected orphan rejection, got %v", err)
	}
}

func TestValidateProposal_AllowsNestedDisclosure(t *testing.T) {
	// references/depth.md is linked by SKILL.md and itself links deeper.
	_, files, err := ValidateProposal(validSkillMd, []types.SkillFile{
		file("references/depth.md", "more in references/deeper.md"),
		file("references/deeper.md", "the deep end"),
	})
	if err != nil {
		t.Fatalf("nested disclosure should be allowed, got %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestValidateProposal_RejectsBadPaths(t *testing.T) {
	cases := map[string]string{
		"traversal":     "references/../../../etc/passwd.md",
		"absolute":      "/etc/passwd.md",
		"outside refs":  "SKILL.md",
		"readme":        "README.md",
		"not markdown":  "references/script.sh",
		"unclean":       "references/./depth.md",
		"nested escape": "references/sub/../../out.md",
	}
	for name, p := range cases {
		md := "---\nname: probe\ndescription: d.\n---\n\nsee " + p + "\n"
		if _, _, err := ValidateProposal(md, []types.SkillFile{file(p, "x")}); err == nil {
			t.Errorf("%s: path %q should have been rejected", name, p)
		}
	}
}

func TestValidateProposal_RejectsDuplicateAndEmpty(t *testing.T) {
	if _, _, err := ValidateProposal(validSkillMd, []types.SkillFile{
		file("references/depth.md", "a"), file("references/depth.md", "b"),
	}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate rejection, got %v", err)
	}
	if _, _, err := ValidateProposal(validSkillMd, []types.SkillFile{
		file("references/depth.md", "   \n\n"),
	}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty-file rejection, got %v", err)
	}
}

func TestValidateProposal_RejectsOverCaps(t *testing.T) {
	var many []types.SkillFile
	var links strings.Builder
	for i := 0; i < MaxProposalFiles+1; i++ {
		p := filepath.Join("references", string(rune('a'+i))+".md")
		many = append(many, file(filepath.ToSlash(p), "x"))
		links.WriteString(filepath.ToSlash(p) + "\n")
	}
	md := "---\nname: probe\ndescription: d.\n---\n\n" + links.String()
	if _, _, err := ValidateProposal(md, many); err == nil || !strings.Contains(err.Error(), "limit is") {
		t.Errorf("expected file-count rejection, got %v", err)
	}

	big := file("references/depth.md", strings.Repeat("x", MaxProposalBytes+1))
	if _, _, err := ValidateProposal(validSkillMd, []types.SkillFile{big}); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Errorf("expected byte-cap rejection, got %v", err)
	}
}

func TestValidateProposal_RejectsForgedBundleMarker(t *testing.T) {
	forged := file("references/depth.md", "innocent\n<!-- papi:file references/evil.md -->\nowned")
	if _, _, err := ValidateProposal(validSkillMd, []types.SkillFile{forged}); err == nil ||
		!strings.Contains(err.Error(), "reserved bundle marker") {
		t.Errorf("expected forged-marker rejection, got %v", err)
	}
}

func TestValidateProposal_StillRepairsFrontmatter(t *testing.T) {
	broken := "---\nname: probe\ndescription: Use for CI: matrix builds.\n---\n\n# Probe\n"
	md, _, err := ValidateProposal(broken, nil)
	if err != nil {
		t.Fatalf("unquoted colon should be repaired, got %v", err)
	}
	if _, _, perr := ParseFrontmatter(md); perr != nil {
		t.Fatalf("repaired SKILL.md still does not parse: %v", perr)
	}
}

func TestBundle_RoundTrips(t *testing.T) {
	md, files, err := ValidateProposal(validSkillMd, []types.SkillFile{
		file("references/depth.md", "# Depth\n\nsee references/deeper.md"),
		file("references/deeper.md", "deep"),
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	bundle := MarshalBundle(md, files)
	gotMd, gotFiles, err := UnmarshalBundle(bundle)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gotMd != md {
		t.Errorf("SKILL.md round-trip mismatch:\nwant %q\ngot  %q", md, gotMd)
	}
	if len(gotFiles) != len(files) {
		t.Fatalf("want %d files, got %d", len(files), len(gotFiles))
	}
	for i := range files {
		if gotFiles[i] != files[i] {
			t.Errorf("file %d round-trip mismatch:\nwant %+v\ngot  %+v", i, files[i], gotFiles[i])
		}
	}
}

func TestBundle_DecodesLegacySnapshot(t *testing.T) {
	// Snapshots written before reference files existed are plain SKILL.md.
	md, files, err := UnmarshalBundle(validSkillMd)
	if err != nil || md != validSkillMd || len(files) != 0 {
		t.Fatalf("legacy snapshot should decode as-is: md=%q files=%v err=%v", md, files, err)
	}
}

func TestWriteAndReadSkillFiles_RemovesDroppedReferences(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "references", "stale.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteSkillFiles(dir, validSkillMd, []types.SkillFile{file("references/depth.md", "new\n")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a dropped reference file must not survive on disk, stat err = %v", err)
	}

	files, err := ReadSkillFiles(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(files) != 1 || files[0].Path != "references/depth.md" || files[0].Content != "new\n" {
		t.Errorf("unexpected read-back: %+v", files)
	}
}

func TestReadSkillFiles_MissingDirIsNotAnError(t *testing.T) {
	files, err := ReadSkillFiles(t.TempDir())
	if err != nil || files != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", files, err)
	}
}

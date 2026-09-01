package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo creates a throwaway git repo and returns it plus the skill dir inside it.
func newRepo(t *testing.T) (*ResearchGit, string) {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "papi@test.invalid"},
		{"config", "user.name", "papi test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	skillDir := filepath.Join(root, "skills", "probe")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	return New(root), skillDir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func exists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

func TestCommitSkillDir_IncludesReferenceFiles(t *testing.T) {
	g, skillDir := newRepo(t)
	write(t, skillDir, "SKILL.md", "v1\n")
	write(t, skillDir, "references/depth.md", "deep\n")

	sha, err := g.CommitSkillDir(skillDir, "baseline")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if sha == "" {
		t.Fatal("expected a commit sha")
	}

	out, err := g.run("show", "--name-only", "--pretty=format:", sha)
	if err != nil {
		t.Fatal(err)
	}
	if want := "skills/probe/references/depth.md"; !strings.Contains(out, want) {
		t.Errorf("commit should include %s, got:\n%s", want, out)
	}
}

func TestCommitSkillDir_NoChangesReturnsHead(t *testing.T) {
	g, skillDir := newRepo(t)
	write(t, skillDir, "SKILL.md", "v1\n")
	first, err := g.CommitSkillDir(skillDir, "baseline")
	if err != nil {
		t.Fatal(err)
	}
	again, err := g.CommitSkillDir(skillDir, "no-op")
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if again != first {
		t.Errorf("an unchanged skill must not create a commit: %s != %s", again, first)
	}
}

// A rejected proposal may add reference files that are not in the best commit.
// checkout alone would leave them on disk for Claude to find; the revert has to
// remove them too.
func TestRevertSkillDir_RemovesFilesAddedByRejectedProposal(t *testing.T) {
	g, skillDir := newRepo(t)
	write(t, skillDir, "SKILL.md", "best\n")
	write(t, skillDir, "references/keep.md", "keep\n")
	best, err := g.CommitSkillDir(skillDir, "best")
	if err != nil {
		t.Fatal(err)
	}

	// A worse proposal: rewrites SKILL.md, drops one reference, adds another.
	write(t, skillDir, "SKILL.md", "worse\n")
	if err := os.Remove(filepath.Join(skillDir, "references", "keep.md")); err != nil {
		t.Fatal(err)
	}
	write(t, skillDir, "references/junk.md", "junk\n")

	if err := g.RevertSkillDir(skillDir, best); err != nil {
		t.Fatalf("revert: %v", err)
	}

	if got := read(t, skillDir, "SKILL.md"); got != "best\n" {
		t.Errorf("SKILL.md = %q, want %q", got, "best\n")
	}
	if got := read(t, skillDir, "references/keep.md"); got != "keep\n" {
		t.Errorf("dropped reference file was not restored: %q", got)
	}
	if exists(skillDir, "references/junk.md") {
		t.Error("reference file added by the rejected proposal survived the revert")
	}
}

// Reverting to a commit from before the skill was split up must leave no
// references/ directory behind at all.
func TestRevertSkillDir_RemovesWholeReferencesTree(t *testing.T) {
	g, skillDir := newRepo(t)
	write(t, skillDir, "SKILL.md", "single file\n")
	best, err := g.CommitSkillDir(skillDir, "single-file baseline")
	if err != nil {
		t.Fatal(err)
	}

	write(t, skillDir, "SKILL.md", "router\n")
	write(t, skillDir, "references/a.md", "a\n")
	write(t, skillDir, "references/nested/b.md", "b\n")

	if err := g.RevertSkillDir(skillDir, best); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if exists(skillDir, "references") {
		t.Error("references/ should be gone after reverting to a single-file commit")
	}
}

// The revert is scoped to the skill dir; nothing else in the repo may be touched.
func TestRevertSkillDir_LeavesRestOfRepoAlone(t *testing.T) {
	g, skillDir := newRepo(t)
	root := filepath.Dir(filepath.Dir(skillDir))
	write(t, skillDir, "SKILL.md", "best\n")
	best, err := g.CommitSkillDir(skillDir, "best")
	if err != nil {
		t.Fatal(err)
	}

	write(t, root, "untracked-scratch.txt", "do not delete me\n")
	write(t, skillDir, "SKILL.md", "worse\n")

	if err := g.RevertSkillDir(skillDir, best); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if !exists(root, "untracked-scratch.txt") {
		t.Error("revert deleted an untracked file outside the skill directory")
	}
}

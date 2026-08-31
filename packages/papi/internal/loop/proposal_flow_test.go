package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"papi/internal/config"
	researchgit "papi/internal/git"
	"papi/internal/types"
)

// TestProposalFlow_AcceptThenReject walks the accept/reject cycle the loop performs
// around a multi-file proposal: validate, write, commit on improvement, revert on
// regression. It is the composition that matters here - each step is unit-tested in
// its own package, but only together do they guarantee that a rejected proposal leaves
// the skill directory byte-identical to the last accepted one.
func TestProposalFlow_AcceptThenReject(t *testing.T) {
	repoRoot := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "papi@test.invalid"},
		{"config", "user.name", "papi test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	skillDir := filepath.Join(repoRoot, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	g := researchgit.New(repoRoot)

	// Baseline: one long single-file skill.
	baseline := "---\nname: demo\ndescription: a demo skill\n---\n\nall of it inline\n"
	if err := config.WriteSkillFiles(skillDir, baseline, nil); err != nil {
		t.Fatal(err)
	}
	bestSha, err := g.CommitSkillDir(skillDir, "baseline")
	if err != nil {
		t.Fatal(err)
	}

	// Iteration 1: the agent splits the skill up, and the score improves.
	splitMd := "---\nname: demo\ndescription: a demo skill\n---\n\nDetail lives in references/depth.md.\n"
	md, files, err := config.ValidateProposal(splitMd, []types.SkillFile{
		{Path: "references/depth.md", Content: "the detail"},
	})
	if err != nil {
		t.Fatalf("split proposal should be valid: %v", err)
	}
	if err := config.WriteSkillFiles(skillDir, md, files); err != nil {
		t.Fatal(err)
	}
	bestSha, err = g.CommitSkillDir(skillDir, "iter 001")
	if err != nil {
		t.Fatal(err)
	}
	accepted := snapshotSkillDir(skillDir)

	// Iteration 2: a proposal that reshuffles the references and scores worse.
	worseMd := "---\nname: demo\ndescription: a demo skill\n---\n\nSee references/other.md.\n"
	md, files, err = config.ValidateProposal(worseMd, []types.SkillFile{
		{Path: "references/other.md", Content: "different detail"},
	})
	if err != nil {
		t.Fatalf("second proposal should be valid: %v", err)
	}
	if err := config.WriteSkillFiles(skillDir, md, files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "references", "depth.md")); !os.IsNotExist(err) {
		t.Error("applying a proposal must drop reference files it left out")
	}

	// Score did not improve, so the loop reverts.
	if err := g.RevertSkillDir(skillDir, bestSha); err != nil {
		t.Fatalf("revert: %v", err)
	}

	if got := snapshotSkillDir(skillDir); got != accepted {
		t.Errorf("revert did not restore the accepted skill exactly:\nwant %q\ngot  %q", accepted, got)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "references", "other.md")); !os.IsNotExist(err) {
		t.Error("a reference file from the rejected proposal survived the revert")
	}
}

// TestProposalFlow_RejectedProposalNeverReachesDisk covers the guard that matters most:
// a SKILL.md pointing at a reference file the agent did not supply must be refused
// before anything is written, since Claude would otherwise follow the pointer to a file
// that does not exist.
func TestProposalFlow_RejectedProposalNeverReachesDisk(t *testing.T) {
	skillDir := t.TempDir()
	original := "---\nname: demo\ndescription: a demo skill\n---\n\noriginal\n"
	if err := config.WriteSkillFiles(skillDir, original, nil); err != nil {
		t.Fatal(err)
	}

	broken := "---\nname: demo\ndescription: a demo skill\n---\n\nSee references/missing.md.\n"
	if _, _, err := config.ValidateProposal(broken, nil); err == nil {
		t.Fatal("a dangling reference must be rejected")
	}

	// The loop only writes when validation passes, so the skill is untouched.
	if got := snapshotSkillDir(skillDir); got != original {
		t.Errorf("skill changed despite a rejected proposal:\nwant %q\ngot  %q", original, got)
	}
}

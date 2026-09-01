package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ResearchGit wraps git CLI operations needed by the research loop.
type ResearchGit struct {
	repoRoot string
}

func New(repoRoot string) *ResearchGit {
	return &ResearchGit{repoRoot: repoRoot}
}

func (g *ResearchGit) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// CommitSkillDir stages the whole skill directory and creates a commit. Returns the
// new SHA. If nothing changed after staging, it skips the commit and returns the
// current HEAD SHA.
//
// The skill is a set of files — SKILL.md plus the references/ it discloses — so the
// commit has to cover the directory, and `add -A` is what records a reference file the
// agent decided to drop.
func (g *ResearchGit) CommitSkillDir(skillDir, message string) (string, error) {
	if _, err := g.run("add", "-A", "--", skillDir); err != nil {
		return "", err
	}
	staged, err := g.run("diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if staged == "" {
		return g.run("rev-parse", "HEAD")
	}
	if _, err := g.run("commit", "-m", message); err != nil {
		return "", err
	}
	sha, err := g.run("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return sha, nil
}

// RevertSkillDir restores the skill directory to a specific commit without touching
// anything else in the repo.
//
// `checkout` alone is not enough: it restores tracked files but leaves behind any
// reference file the rejected proposal newly created, which Claude would still find on
// disk. `clean -fd` removes those. It is scoped to skillDir and deliberately omits -x,
// so ignored files elsewhere (run artifacts under .papi/) are never in range.
func (g *ResearchGit) RevertSkillDir(skillDir, sha string) error {
	if _, err := g.run("checkout", sha, "--", skillDir); err != nil {
		return err
	}
	_, err := g.run("clean", "-fd", "--", skillDir)
	return err
}

// CreateTag creates a lightweight tag at HEAD.
func (g *ResearchGit) CreateTag(tag string) error {
	_, err := g.run("tag", tag)
	return err
}

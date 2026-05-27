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

// CommitSkill stages SKILL.md and creates a commit. Returns the new SHA.
// If nothing changed after staging, it skips the commit and returns the current HEAD SHA.
func (g *ResearchGit) CommitSkill(skillMdPath, message string) (string, error) {
	if _, err := g.run("add", skillMdPath); err != nil {
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

// RevertSkillFile checks out SKILL.md from a specific commit without touching anything else.
func (g *ResearchGit) RevertSkillFile(skillMdPath, sha string) error {
	_, err := g.run("checkout", sha, "--", skillMdPath)
	return err
}

// CreateTag creates a lightweight tag at HEAD.
func (g *ResearchGit) CreateTag(tag string) error {
	_, err := g.run("tag", tag)
	return err
}

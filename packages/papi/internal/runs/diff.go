package runs

import "github.com/aymanbagabas/go-udiff"

// DiffSkillMd returns a unified diff between two SKILL.md snapshots. An empty
// result means the files are identical.
func DiffSkillMd(prev, cur string) string {
	return udiff.Unified("previous", "proposed", prev, cur)
}

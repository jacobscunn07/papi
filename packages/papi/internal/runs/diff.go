package runs

import "github.com/aymanbagabas/go-udiff"

// DiffSkillMd returns a unified diff between two skill snapshots (SKILL.md plus
// its reference files, in bundle form). An empty result means they are identical.
func DiffSkillMd(prev, cur string) string {
	return udiff.Unified("previous", "proposed", prev, cur)
}

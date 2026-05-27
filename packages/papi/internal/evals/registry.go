package evals

import (
	"fmt"

	"papi/internal/types"
)

// NewRegistry returns the full set of evals for a research run.
// Built-in evals are always included; TypeScript evals are discovered from customEvalsDir.
func NewRegistry(customEvalsDir string) []types.Eval {
	evalList := []types.Eval{
		NewSkillUsedEval(),
		NewOutputQualityEval(),
	}
	evalList = append(evalList, discoverEvals(customEvalsDir)...)
	ids := make([]string, len(evalList))
	for i, e := range evalList {
		ids[i] = e.ID()
	}
	fmt.Printf("Loaded evals: %s\n", joinStrings(ids, ", "))
	return evalList
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

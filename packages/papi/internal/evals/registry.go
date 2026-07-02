package evals

import (
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
	return evalList
}

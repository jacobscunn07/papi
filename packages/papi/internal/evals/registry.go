package evals

import (
	"papi/internal/types"
)

// NewRegistry returns the full set of evals for a research run.
// Built-in evals are always included; custom TypeScript/JavaScript evals are
// discovered from customEvalsDir.
func NewRegistry(customEvalsDir string) ([]types.Eval, error) {
	evalList := []types.Eval{
		NewSkillUsedEval(),
		NewOutputQualityEval(),
	}
	custom, err := discoverEvals(customEvalsDir)
	if err != nil {
		return nil, err
	}
	return append(evalList, custom...), nil
}

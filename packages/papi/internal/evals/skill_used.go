package evals

import (
	"fmt"

	"papi/internal/types"
)

type skillUsedEval struct{}

func NewSkillUsedEval() types.Eval { return &skillUsedEval{} }

func (e *skillUsedEval) ID() string        { return "skill-used" }
func (e *skillUsedEval) Name() string      { return "Skill Was Invoked" }
func (e *skillUsedEval) IsLLMJudge() bool  { return false }

func (e *skillUsedEval) Evaluate(ctx types.EvalContext) (types.EvalResult, error) {
	shouldInvoke := ctx.Scenario.ShouldInvoke == nil || *ctx.Scenario.ShouldInvoke

	if shouldInvoke {
		if ctx.Invoked {
			return types.EvalResult{
				EvalID:    "skill-used",
				Name:      "Skill Was Invoked",
				Score:     1.0,
				Reasoning: fmt.Sprintf("Claude invoked /%s during the invocation check (description-only).", ctx.SkillName),
				Required:  true,
			}, nil
		}
		return types.EvalResult{
			EvalID: "skill-used",
			Name:   "Skill Was Invoked",
			Score:  0.0,
			Reasoning: fmt.Sprintf(
				"Claude did not invoke /%s when shown only the description. "+
					"The description may be too vague or not match the scenario's language.",
				ctx.SkillName,
			),
			Required: true,
		}, nil
	}

	// Negative scenario: skill should NOT be invoked
	if !ctx.Invoked {
		return types.EvalResult{
			EvalID:    "skill-used",
			Name:      "Skill Correctly Not Invoked",
			Score:     1.0,
			Reasoning: fmt.Sprintf("Correct — Claude did not invoke /%s for an unrelated task.", ctx.SkillName),
			Required:  true,
		}, nil
	}
	return types.EvalResult{
		EvalID: "skill-used",
		Name:   "Skill Correctly Not Invoked",
		Score:  0.0,
		Reasoning: fmt.Sprintf(
			"The description is too broad — Claude incorrectly invoked /%s for a task that has nothing to do with this skill.",
			ctx.SkillName,
		),
		Required: true,
	}, nil
}

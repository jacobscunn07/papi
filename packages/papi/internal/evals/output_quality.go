package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strings"

	"papi/internal/types"
)

type outputQualityEval struct{}

func NewOutputQualityEval() types.Eval { return &outputQualityEval{} }

func (e *outputQualityEval) ID() string        { return "output-quality" }
func (e *outputQualityEval) Name() string      { return "Output Quality" }
func (e *outputQualityEval) Weight() float64   { return 1.0 }
func (e *outputQualityEval) IsLLMJudge() bool  { return true }

func (e *outputQualityEval) Evaluate(ctx types.EvalContext) (types.EvalResult, error) {
	if !ctx.Invoked || ctx.QualityTranscript == "" {
		return types.EvalResult{
			EvalID:    "output-quality",
			Name:      "Output Quality",
			Score:     0.0,
			Reasoning: "Skipped — skill was not invoked in the invocation check.",
		}, nil
	}

	judgePrompt := fmt.Sprintf(`You are evaluating the quality of an AI assistant's response to a user task.

TASK:
%s

ASSISTANT RESPONSE:
%s

Score the response from 0 to 1 on these criteria:
- Specificity: does it give concrete, actionable guidance (not generic advice)?
- Structure: is it well-organized with clear sections/steps?
- Completeness: does it address the core of the task?

Score guide:
- 0.0–0.3: Generic, vague, or unhelpful
- 0.4–0.6: Partially helpful with some specifics
- 0.7–0.9: Solid, specific, actionable
- 1.0: Exceptional — highly specific, well-structured, complete

Respond with JSON only: {"score": <0-1>, "reasoning": "<one sentence>"}`,
		ctx.Scenario.Prompt,
		ctx.QualityTranscript,
	)

	score, reasoning, err := judgeWithClaude(judgePrompt)
	if err != nil {
		return types.EvalResult{}, fmt.Errorf("output-quality judge: %w", err)
	}

	return types.EvalResult{
		EvalID:    "output-quality",
		Name:      "Output Quality",
		Score:     math.Max(0, math.Min(1, score)),
		Reasoning: reasoning,
	}, nil
}

var jsonObjectRe = regexp.MustCompile(`\{[^{}]*"score"[^{}]*\}`)

func judgeWithClaude(prompt string) (score float64, reasoning string, err error) {
	cmd := exec.Command("claude",
		"-p", prompt,
		"--model", "claude-haiku-4-5-20251001",
		"--output-format", "json",
		"--no-session-persistence",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return 0, "", fmt.Errorf("judge exec: %w\n%s", err, stderr.String())
	}

	var outer struct {
		Result string `json:"result"`
	}
	if err = json.Unmarshal(stdout.Bytes(), &outer); err != nil {
		return 0, "", fmt.Errorf("parse outer json: %w", err)
	}

	raw := strings.TrimSpace(outer.Result)
	if strings.HasPrefix(raw, "```") {
		raw = regexp.MustCompile("^```(?:\\w+)?\\n?").ReplaceAllString(raw, "")
		raw = regexp.MustCompile("\\n?```$").ReplaceAllString(raw, "")
	}

	var parsed struct {
		Score     float64 `json:"score"`
		Reasoning string  `json:"reasoning"`
	}
	if jsonErr := json.Unmarshal([]byte(raw), &parsed); jsonErr != nil {
		if m := jsonObjectRe.FindString(raw); m != "" {
			if jsonErr2 := json.Unmarshal([]byte(m), &parsed); jsonErr2 == nil {
				return parsed.Score, parsed.Reasoning, nil
			}
		}
		return 0.5, "LLM judge did not return valid JSON.", nil
	}
	return parsed.Score, parsed.Reasoning, nil
}

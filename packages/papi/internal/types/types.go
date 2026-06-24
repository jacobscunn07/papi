package types

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// HookList is an ordered list of hook script paths. It unmarshals from either a
// YAML scalar string (single hook, backwards compatible) or a YAML sequence.
type HookList []string

func (h *HookList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Value != "" {
			*h = HookList{value.Value}
		}
		return nil
	case yaml.SequenceNode:
		var scripts []string
		if err := value.Decode(&scripts); err != nil {
			return err
		}
		*h = HookList(scripts)
		return nil
	}
	return fmt.Errorf("hooks: expected string or list, got %v", value.Tag)
}

// Scenario represents a single evaluation scenario from scenarios.yaml.
type Scenario struct {
	ID           string            `yaml:"id"           json:"id"`
	Prompt       string            `yaml:"prompt"       json:"prompt"`
	Fixtures     map[string]string `yaml:"fixtures"     json:"fixtures"`
	Tags         []string          `yaml:"tags"         json:"tags"`
	ShouldInvoke *bool             `yaml:"shouldInvoke" json:"shouldInvoke"`
}

// Hooks holds lifecycle hook script lists for a skill. Each field accepts either
// a single script path (string) or an ordered list of script paths ([]string).
type Hooks struct {
	PreRun        HookList `yaml:"pre-run"        json:"pre-run"`
	PostRun       HookList `yaml:"post-run"       json:"post-run"`
	PreIteration  HookList `yaml:"pre-iteration"  json:"pre-iteration"`
	PostIteration HookList `yaml:"post-iteration" json:"post-iteration"`
	PreScenario   HookList `yaml:"pre-scenario"   json:"pre-scenario"`
	PostScenario  HookList `yaml:"post-scenario"  json:"post-scenario"`
	PreEval       HookList `yaml:"pre-eval"       json:"pre-eval"`
	PostEval      HookList `yaml:"post-eval"      json:"post-eval"`
	PostQuality   HookList `yaml:"post-quality"   json:"post-quality"`
}

// ScenarioFile is the top-level structure of scenarios.yaml.
type ScenarioFile struct {
	Skill     string     `yaml:"skill"`
	Hooks     *Hooks     `yaml:"hooks"`
	Scenarios []Scenario `yaml:"scenarios"`
}

// ClaudeJsonOutput is the JSON structure returned by `claude --output-format json`.
type ClaudeJsonOutput struct {
	Type        string  `json:"type"`
	Subtype     string  `json:"subtype"`
	IsError     bool    `json:"is_error"`
	Result      string  `json:"result"`
	SessionID   string  `json:"session_id"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns    int     `json:"num_turns"`
	Usage       struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// EvalContext carries all information available to an eval function.
type EvalContext struct {
	Scenario             Scenario          `json:"scenario"`
	InvocationTranscript string            `json:"invocationTranscript"`
	InvocationOutput     *ClaudeJsonOutput `json:"invocationOutput"`
	QualityTranscript    string            `json:"qualityTranscript"`
	QualityOutput        *ClaudeJsonOutput `json:"qualityOutput"`
	SkillName            string            `json:"skillName"`
	SkillDescription     string            `json:"skillDescription"`
	SkillContent         string            `json:"skillContent"`
	SkillDir             string            `json:"skillDir"`
	WorkDir              string            `json:"workDir"`
	Invoked              bool              `json:"invoked"`
}

// EvalResult holds the outcome of a single eval.
type EvalResult struct {
	EvalID     string  `json:"evalId"`
	Name       string  `json:"name"`
	Score      float64 `json:"score"`
	Reasoning  string  `json:"reasoning"`
	Required   bool    `json:"required,omitempty"`
	IsLLMJudge bool    `json:"isLLMJudge,omitempty"`
}

// Eval is the interface every evaluator must implement.
type Eval interface {
	ID() string
	Name() string
	IsLLMJudge() bool
	Evaluate(ctx EvalContext) (EvalResult, error)
}

// ScenarioRunResult holds the full output of running one scenario.
type ScenarioRunResult struct {
	Scenario         Scenario          `json:"scenario"`
	InvocationOutput *ClaudeJsonOutput `json:"invocationOutput"`
	QualityOutput    *ClaudeJsonOutput `json:"qualityOutput"`
	Invoked          bool              `json:"invoked"`
	EvalResults      []EvalResult      `json:"evalResults"`
	ScenarioScore    float64           `json:"scenarioScore"`
	DurationMs       int64             `json:"durationMs"`
}

// ResearchConfig holds all runtime configuration for the loop.
type ResearchConfig struct {
	SkillName      string
	SkillDir       string
	ScenariosDir   string
	CustomEvalsDir string
	MaxIterations  int
	MaxBudgetUSD   float64
	Tags           []string
	DryRun         bool
	ScenarioModel string // Invocation Phase model — description-only check (default: claude-haiku-4-5-20251001)
	QualityModel  string // Quality Phase model — full skill execution (default: claude-sonnet-4-6)
	ResearchModel string // Research agent improvement loop (default: claude-opus-4-7)
	MaxRuns          int     // max runs to retain per skill; 0 = keep all
	LLMJudgeWeight   float64 // category weight for LLM judge evals (default 0.30)
	NonLLMJudgeWeight float64 // category weight for non-LLM judge evals (default 0.70)
}

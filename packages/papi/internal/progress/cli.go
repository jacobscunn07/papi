package progress

import (
	"fmt"
	"strings"

	"papi/internal/types"
)

// CLIReporter formats events to stdout, reproducing the original headless output
// of the research loop.
type CLIReporter struct {
	maxIter      int
	llmWeight    float64
	nonLLMWeight float64
}

// NewCLIReporter returns a reporter that prints to stdout. maxIter and the eval
// category weights are needed to format iteration headers and eval breakdowns.
func NewCLIReporter(maxIter int, llmWeight, nonLLMWeight float64) *CLIReporter {
	return &CLIReporter{maxIter: maxIter, llmWeight: llmWeight, nonLLMWeight: nonLLMWeight}
}

func pct(s float64) string { return fmt.Sprintf("%.1f", s*100) }

func (r *CLIReporter) Emit(e Event) {
	bar := strings.Repeat("━", 55)
	switch ev := e.(type) {
	case RunStarted:
		fmt.Printf("\n%s\n", bar)
		fmt.Printf("Skill: %s    Max: %d iterations    Budget: $%.2f\n", ev.Skill, ev.MaxIterations, ev.Budget)
		fmt.Printf("%s\n", bar)
		fmt.Printf("Scenarios (%d): %s\n", len(ev.ScenarioIDs), strings.Join(ev.ScenarioIDs, ", "))

	case IterationStarted:
		if ev.Iter == 0 {
			fmt.Println("\n══ BASELINE ══")
		} else {
			fmt.Printf("\n══ ITERATION %d/%d  (best: %s) ══\n", ev.Iter, r.maxIter, pct(ev.Best))
		}

	case ResearchAgentDone:
		fmt.Printf("  Calling research agent... $%.4f\n", ev.Cost)
		if ev.Description != "" {
			fmt.Printf("  Experiment: %q\n", ev.Description)
		}

	case ScenarioStarted:
		fmt.Printf("  ▸ %s  ", ev.ID)

	case ScenarioDone:
		printScenarioResult(ev.Result, r.llmWeight, r.nonLLMWeight)

	case IterationDone:
		if ev.Iter == 0 {
			fmt.Printf("  Aggregate: %s  |  Cost: $%.4f  |  %s\n", pct(ev.Score), ev.Cost, FmtDuration(ev.DurationMs))
		} else {
			deltaDisplay := pct(ev.Delta)
			if ev.Delta > 0 {
				deltaDisplay = "+" + deltaDisplay
			}
			fmt.Printf("  Aggregate: %s  (Δ %s)  |  Cost: $%.4f  |  %s\n", pct(ev.Score), deltaDisplay, ev.Cost, FmtDuration(ev.DurationMs))
		}
		printScenarioBreakdown(ev.Results)

	case RunDone:
		fmt.Printf("\n%s\n", bar)
		fmt.Printf("Done. Best score: %s | Total cost: $%.4f | Duration: %s\n", pct(ev.Best), ev.Cost, FmtDuration(ev.DurationMs))
		if ev.Tag != "" {
			fmt.Printf("Tagged: %s\n", ev.Tag)
		}
		fmt.Printf("%s\n", bar)

	case LogLine:
		fmt.Println(ev.Text)
	}
}

func categorize(results []types.EvalResult) (nonLLM, llm []types.EvalResult) {
	for _, e := range results {
		if e.IsLLMJudge {
			llm = append(llm, e)
		} else {
			nonLLM = append(nonLLM, e)
		}
	}
	return
}

func groupScore(evals []types.EvalResult) float64 {
	if len(evals) == 0 {
		return 0
	}
	var sum float64
	for _, e := range evals {
		sum += e.Score
	}
	return sum / float64(len(evals))
}

func printEvalGroup(evals []types.EvalResult) {
	for i, e := range evals {
		branch := "├─"
		if i == len(evals)-1 {
			branch = "└─"
		}
		name := e.Name
		if e.Required {
			name += " [required]"
		}
		if e.IsLLMJudge {
			name += " [llm]"
		}
		fmt.Printf("    %s %-36s %6.1f  %7s   %q\n", branch, name, e.Score*100, FmtDuration(e.DurationMs), e.Reasoning)
	}
}

func printScenarioResult(result types.ScenarioRunResult, llmWeight, nonLLMWeight float64) {
	invokedLabel := "INVOKED"
	if !result.Invoked {
		shouldInvoke := result.Scenario.ShouldInvoke == nil || *result.Scenario.ShouldInvoke
		if shouldInvoke {
			invokedLabel = "NOT INVOKED"
		} else {
			invokedLabel = "NOT INVOKED ✓"
		}
	}
	fmt.Printf("[%s]  score: %s\n", invokedLabel, pct(result.ScenarioScore))

	nonLLMEvals, llmEvals := categorize(result.EvalResults)
	nonLLMScore := groupScore(nonLLMEvals)
	llmScore := groupScore(llmEvals)

	if len(nonLLMEvals) > 0 {
		fmt.Printf("    ── non-llm  %.0f%% ──────────────────────────  %5.1f\n", nonLLMWeight*100, nonLLMScore*100)
		printEvalGroup(nonLLMEvals)
		fmt.Println()
	}
	if len(llmEvals) > 0 {
		fmt.Printf("    ── llm  %.0f%% ─────────────────────────────  %5.1f\n", llmWeight*100, llmScore*100)
		printEvalGroup(llmEvals)
		fmt.Println()
	}
}

func printScenarioBreakdown(results []types.ScenarioRunResult) {
	for i, r := range results {
		branch := "├─"
		if i == len(results)-1 {
			branch = "└─"
		}
		fmt.Printf("    %s %-32s %6.1f  %7s\n", branch, r.Scenario.ID, r.ScenarioScore*100, FmtDuration(r.DurationMs))
	}
}

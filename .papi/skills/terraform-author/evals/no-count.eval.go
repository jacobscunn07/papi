package main

import (
	"encoding/json"
	"os"
	"regexp"
)

type scenario struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

type evalContext struct {
	Scenario          scenario `json:"scenario"`
	QualityTranscript string   `json:"qualityTranscript"`
	Invoked           bool     `json:"invoked"`
}

type evalResult struct {
	EvalID    string  `json:"evalId"`
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
	Reasoning string  `json:"reasoning"`
}

const evalID = "no-count"
const evalName = "No count — use for_each"

var (
	codeBlockRe   = regexp.MustCompile("(?s)```[\\w]*\n([\\s\\S]*?)```")
	commentRe     = regexp.MustCompile(`(?m)#[^\n]*`)
	createCountRe = regexp.MustCompile(`count\s*=\s*var\.create\w*`)
	countRe       = regexp.MustCompile(`\bcount\s*=`)
	forEachRe     = regexp.MustCompile(`\bfor_each\s*=`)
)

func extractCodeBlocks(text string) []string {
	matches := codeBlockRe.FindAllStringSubmatch(text, -1)
	blocks := make([]string, 0, len(matches))
	for _, m := range matches {
		blocks = append(blocks, m[1])
	}
	return blocks
}

func isCountViolation(code string) bool {
	cleaned := createCountRe.ReplaceAllString(code, "")
	return countRe.MatchString(cleaned)
}

func emit(score float64, reasoning string) {
	json.NewEncoder(os.Stdout).Encode(evalResult{
		EvalID:    evalID,
		Name:      evalName,
		Score:     score,
		Reasoning: reasoning,
	})
}

func main() {
	var ctx evalContext
	if err := json.NewDecoder(os.Stdin).Decode(&ctx); err != nil {
		os.Stderr.WriteString("decode: " + err.Error() + "\n")
		os.Exit(1)
	}

	if !ctx.Invoked || ctx.QualityTranscript == "" {
		emit(0.0, "Skipped — skill not invoked.")
		return
	}

	blocks := extractCodeBlocks(ctx.QualityTranscript)
	hasForEach := false

	for _, block := range blocks {
		stripped := commentRe.ReplaceAllString(block, "")
		if isCountViolation(stripped) {
			emit(0.1, "Response contains `count =` in a code example. Use `for_each` instead.")
			return
		}
		if forEachRe.MatchString(stripped) {
			hasForEach = true
		}
	}

	if len(blocks) > 0 && hasForEach {
		emit(1.0, "Code uses `for_each` with no `count =` violations.")
		return
	}
	if len(blocks) > 0 {
		emit(0.6, "Code blocks present but no `for_each` usage detected.")
		return
	}
	emit(0.5, "No code blocks found — cannot determine for_each usage.")
}

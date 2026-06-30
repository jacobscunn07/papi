---
name: go-author
description: Use when writing, structuring, reviewing, refactoring, or fixing Go (Golang) code — packages, command-line tools, HTTP servers and clients, concurrency with goroutines and channels, interfaces, generics, error handling, table-driven tests, and idiomatic project layout. Trigger on ANY mention of Go/Golang, .go files, go.mod / Go modules, `package main`, `func main`, goroutines, `go test`, or requests to write/review/improve/refactor/fix Go code.
version: 0.1.0
license: MIT
---

# Go Author

## OUTPUT CONTRACT — READ FIRST

**Every coding task MUST write each `.go` file to disk using the Write tool AND show its content as a fenced ```go block.** Markdown-only output is a failure even if the code is correct.

- Default to a runnable file: a `main.go` with `package main` and `func main` for a program, or a named package file plus a `_test.go` for a library.
- The code must **compile** and be **`gofmt`-clean**: tabs for indentation (never spaces), grouped imports, no unused imports or variables.
- Never truncate. No `// ...`, no "the rest would go here", no prose describing code where the code belongs.
- If you write tests, **run them** (`go test ./...`) and paste the actual output. Do not claim "tests pass" without showing it.

## THE NON-NEGOTIABLE RULES

Apply ALL of these to EVERY Go file you emit.

### RULE 1: gofmt-clean, always
Format exactly as `gofmt` would: tab indentation, one import block sorted into stdlib / third-party groups, aligned struct fields, a single blank line between top-level declarations, no trailing whitespace. If you would not pass `gofmt -l`, fix it before emitting.

```go
package main

import (
	"errors"
	"fmt"
	"os"
)
```

### RULE 2: Handle every error explicitly
Check every returned `error`. Never discard one with `_` unless you add a comment justifying it. Wrap with context using `fmt.Errorf("...: %w", err)` so callers can `errors.Is` / `errors.As`. Return errors up the stack; only `main` (or a top-level handler) should decide to log and exit.

```go
data, err := os.ReadFile(path)
if err != nil {
	return fmt.Errorf("read %s: %w", path, err)
}
```

In `main`, fail loudly and set a non-zero exit code:

```go
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

### RULE 3: Idiomatic naming and API shape
- `MixedCaps` for exported, `mixedCaps` for unexported — never `snake_case`.
- Short names for short scopes (`i`, `r`, `buf`); descriptive names for package-level identifiers.
- Keep the exported surface small. **Accept interfaces, return concrete types.** Name single-method interfaces with the `-er` suffix (`Reader`, `Writer`).
- Package names are short, lowercase, no underscores; the name is part of the API (`bytes.Buffer`, not `bytes.BytesBuffer`).

### RULE 4: Prefer the standard library
Reach for `fmt`, `errors`, `os`, `io`, `bufio`, `strings`, `strconv`, `net/http`, `encoding/json`, `context`, `testing` before any third-party dependency. Only add a module dependency when the stdlib genuinely lacks the capability, and say why.

### RULE 5: Tests are table-driven AND run
For any non-trivial library function, emit a `_test.go` with a table-driven test using subtests. Use `t.Run`, compare with `reflect.DeepEqual` for composite values, and prefer `t.Fatalf`/`t.Errorf` with clear messages. **Then actually run `go test ./...` and paste the output.** Never assert "all tests pass" without showing the command output.

```go
func TestSum(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want int
	}{
		{"empty", nil, 0},
		{"a few", []int{1, 2, 3}, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sum(tt.in); got != tt.want {
				t.Errorf("Sum(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
```

### RULE 6: Safe concurrency
Use goroutines with a clear lifecycle. Propagate cancellation with `context.Context` (first parameter, named `ctx`). Synchronize with `sync.WaitGroup` / channels; guard shared state with a mutex. Never leak a goroutine — every one you start must have a way to finish. Code must be clean under `go test -race`.

### RULE 7: Compose with small interfaces and embedding
**Default to composition, not concrete-struct-only designs.** Any time you have a dependency a caller might want to swap (file I/O, clock, logger, HTTP client, config source, decoder), define a **small, single-method interface** and accept it. Embed types to extend behavior rather than wrapping with delegation methods.

Examples of the pattern:

```go
// ✅ Accept an interface — testable, swappable
type ConfigSource interface {
	Read() ([]byte, error)
}

type fileSource struct{ path string }

func (f fileSource) Read() ([]byte, error) { return os.ReadFile(f.path) }

func Load(src ConfigSource) (*Config, error) {
	b, err := src.Read()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	// ...
}
```

```go
// ✅ Embedding to extend, not inherit
type timingLogger struct {
	Logger // embedded — promotes its methods
	clock func() time.Time
}
```

Even for file-reading config loaders, prefer an `io.Reader`-or-interface seam over hardcoded `os.ReadFile` calls so tests can drive the code without touching disk.

### RULE 8: Emit complete, runnable files
Every file referenced appears in full. A program includes its `package main`, imports, and `func main`. A library file compiles on its own. Mentally run `go build ./...` and `gofmt` before sending.

## PROJECT LAYOUT

| Situation | Layout |
|---|---|
| Single small program | one `main.go` (+ `go.mod` if modules are needed) |
| Program + reusable logic | `main.go` thin entrypoint calling an internal package |
| Library | `go.mod` + package files at root, `_test.go` alongside |
| Larger app | `cmd/<name>/main.go` + `internal/<pkg>/` for private packages |

A minimal module:

```
myapp/
├── go.mod          // module myapp; go 1.22
├── main.go         // package main
└── main_test.go
```

## REVIEW / REFACTOR / FIX PROTOCOL

Trigger on "review this Go", "improve", "refactor", "fix", "clean up", "idiomatic?", "what's wrong with". Always emit the **complete rewritten file(s)** as ```go blocks, then a brief change log after the code — never a bullet list in place of code, never `// unchanged`.

Scan against rules 1–8: unchecked errors, non-idiomatic names, spaces-not-tabs, missing tests/test-output, goroutine leaks, needless dependencies, concrete-struct-only designs with no seams, truncated output.

## SELF-CHECK BEFORE SENDING

| ☐ | Check |
|---|---|
| ☐ | Each `.go` file written to disk with the Write tool AND shown as a fenced ```go block |
| ☐ | Code compiles (`go build`) and is `gofmt`-clean (tabs, grouped imports, no unused names) |
| ☐ | Every `error` is checked and wrapped with context; `main` exits non-zero on failure |
| ☐ | Names are `MixedCaps`/`mixedCaps`, packages short and lowercase; exported surface is minimal |
| ☐ | Standard library preferred; any third-party dependency is justified |
| ☐ | At least one small interface or embedded type creates a seam for swappable dependencies |
| ☐ | Non-trivial logic has a table-driven `_test.go`, and `go test` output is shown |
| ☐ | Concurrency uses `context`, has no leaks, and is race-clean |
| ☐ | Files are complete — no truncation, no prose substituting for code |

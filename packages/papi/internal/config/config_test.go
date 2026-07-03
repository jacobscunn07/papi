package config

import "testing"

// The real frontmatter that crashed a run: an unquoted description containing
// ": " sequences (e.g. `runs: using: node20`). Backticks are not YAML quotes,
// so the plain scalar is invalid YAML.
const badFrontmatter = "---\n" +
	"name: github-actions-author\n" +
	"description: Use whenever the user asks to create a workflow (`runs: using: node20`), composite actions (`runs: using: composite`), and pinning actions to SHAs.\n" +
	"version: 1\n" +
	"---\n" +
	"# body\n"

func TestParseFrontmatter_RejectsUnquotedColon(t *testing.T) {
	if _, _, err := ParseFrontmatter(badFrontmatter); err == nil {
		t.Fatal("expected ParseFrontmatter to fail on unquoted colon-space description")
	}
}

func TestRepairFrontmatter_FixesUnquotedColon(t *testing.T) {
	fixed, ok := RepairFrontmatter(badFrontmatter)
	if !ok {
		t.Fatal("expected RepairFrontmatter to succeed")
	}
	data, _, err := ParseFrontmatter(fixed)
	if err != nil {
		t.Fatalf("repaired frontmatter still fails to parse: %v", err)
	}
	const wantDesc = "Use whenever the user asks to create a workflow (`runs: using: node20`), composite actions (`runs: using: composite`), and pinning actions to SHAs."
	if got, _ := data["description"].(string); got != wantDesc {
		t.Fatalf("description not preserved intact:\n got:  %q\n want: %q", got, wantDesc)
	}
	if got, _ := data["name"].(string); got != "github-actions-author" {
		t.Fatalf("name not preserved: %q", got)
	}
}

func TestRepairFrontmatter_NoOpOnValid(t *testing.T) {
	valid := "---\nname: foo\ndescription: 'already: quoted'\n---\nbody\n"
	fixed, ok := RepairFrontmatter(valid)
	if !ok {
		t.Fatal("expected valid frontmatter to be accepted")
	}
	if fixed != valid {
		t.Fatalf("valid frontmatter should be returned unchanged:\n got:  %q\n want: %q", fixed, valid)
	}
}

func TestRepairFrontmatter_UnrepairableReturnsFalse(t *testing.T) {
	// Missing closing delimiter — not something quoting can fix.
	unclosed := "---\nname: foo\ndescription: bar\n"
	if _, ok := RepairFrontmatter(unclosed); ok {
		t.Fatal("expected unrepairable frontmatter to return ok=false")
	}
}

func TestRepairFrontmatter_PreservesBody(t *testing.T) {
	fixed, ok := RepairFrontmatter(badFrontmatter)
	if !ok {
		t.Fatal("expected repair to succeed")
	}
	_, body, err := ParseFrontmatter(fixed)
	if err != nil {
		t.Fatalf("parse after repair: %v", err)
	}
	if body != "# body\n" {
		t.Fatalf("body not preserved: %q", body)
	}
}

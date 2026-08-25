package script

import (
	"reflect"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		path    string
		want    []string
		wantErr bool
	}{
		{path: "/tmp/hooks/pre-run.ts", want: []string{"tsx"}},
		{path: "/tmp/evals/foo.eval.js", want: []string{"node"}},
		{path: "relative/post-run.ts", want: []string{"tsx"}},
		{path: "/tmp/hooks/pre-run.sh", wantErr: true},
		{path: "/tmp/hooks/post-run.py", wantErr: true},
		{path: "/tmp/evals/no-count.eval.go", wantErr: true},
		{path: "/tmp/hooks/pre-run", wantErr: true},
		{path: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := Resolve(tt.path)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Resolve(%q): expected error, got %v", tt.path, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Resolve(%q): unexpected error: %v", tt.path, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Resolve(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

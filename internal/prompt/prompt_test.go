package prompt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func ask(t *testing.T, typed string, q Question) (string, string) {
	t.Helper()
	var out strings.Builder
	ans, err := NewTerminal(strings.NewReader(typed), &out).Ask(context.Background(), []Question{q})
	if err != nil {
		t.Fatalf("Ask: %v (output %q)", err, out.String())
	}
	return ans[q.ID], out.String()
}

func TestInput(t *testing.T) {
	notBob := func(s string) error {
		if s == "bob" {
			return errors.New("not bob")
		}
		return nil
	}
	tests := []struct {
		name   string
		q      Question
		typed  string
		want   string
		shown  string // expected somewhere in the output
		prompt int    // how many times the question is shown
	}{
		{"typed answer", Question{ID: "x", Text: "Name", Input: true}, "Ada\n", "Ada", "Name: ", 1},
		{"Enter keeps default", Question{ID: "x", Text: "Name", Input: true, Default: "Ada"}, "\n", "Ada", "Name [Ada]: ", 1},
		{"typed answer replaces default", Question{ID: "x", Text: "Name", Input: true, Default: "Ada"}, "Grace\n", "Grace", "", 1},
		{"empty without default asks again", Question{ID: "x", Text: "Name", Input: true}, "\nAda\n", "Ada", "Please type an answer.", 2},
		{"rejected answer asks again", Question{ID: "x", Text: "Name", Input: true, Validate: notBob}, "bob\nAda\n", "Ada", "not bob", 2},
		{"spaces trimmed", Question{ID: "x", Text: "Name", Input: true}, "  Ada  \n", "Ada", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, out := ask(t, tt.typed, tt.q)
			if got != tt.want {
				t.Errorf("answer = %q, want %q", got, tt.want)
			}
			if !strings.Contains(out, tt.shown) {
				t.Errorf("output %q does not contain %q", out, tt.shown)
			}
			if n := strings.Count(out, tt.q.Text); n != tt.prompt {
				t.Errorf("question shown %d times, want %d (output %q)", n, tt.prompt, out)
			}
		})
	}
}

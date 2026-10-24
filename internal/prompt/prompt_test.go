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

func TestOptionalInput(t *testing.T) {
	got, _ := ask(t, "\n", Question{ID: "x", Text: "User", Input: true, Optional: true})
	if got != "" {
		t.Errorf("answer = %q, want empty", got)
	}
}

func TestRepeat(t *testing.T) {
	existing := func(e map[string]string) bool { return e["name"] == "pi" }
	notKept := func(e map[string]string) bool { return e["replace"] != "no" }
	group := Question{
		ID: "hosts", Text: "Add a host?", Again: "Add another?",
		Repeat: []Question{
			{ID: "name", Text: "Name", Input: true},
			{ID: "replace", Text: "Replace it?", Default: "no", When: existing},
			{ID: "addr", Text: "Address", Input: true, When: notKept},
		},
	}

	t.Run("several entries, conditional questions", func(t *testing.T) {
		// box: new name, gets an address. pi: exists, user keeps it, so no address.
		typed := "y\nbox\n10.0.0.1\ny\npi\nn\nn\n"
		var out strings.Builder
		ans, err := NewTerminal(strings.NewReader(typed), &out).Ask(context.Background(), []Question{group})
		if err != nil {
			t.Fatal(err)
		}
		got := ans.Entries("hosts")
		if len(got) != 2 {
			t.Fatalf("entries = %v, want 2", got)
		}
		if got[0]["name"] != "box" || got[0]["addr"] != "10.0.0.1" {
			t.Errorf("entry 0 = %v", got[0])
		}
		if _, asked := got[0]["replace"]; asked {
			t.Error("asked to replace a name that doesn't exist")
		}
		if got[1]["name"] != "pi" || got[1]["replace"] != "no" {
			t.Errorf("entry 1 = %v", got[1])
		}
		if _, asked := got[1]["addr"]; asked {
			t.Error("asked for an address after the user chose to keep the existing host")
		}
		if !strings.Contains(out.String(), "Add another?") {
			t.Errorf("second round should ask %q; output %q", "Add another?", out.String())
		}
	})

	t.Run("no entries", func(t *testing.T) {
		ans, err := NewTerminal(strings.NewReader("\n"), &strings.Builder{}).Ask(context.Background(), []Question{group})
		if err != nil {
			t.Fatal(err)
		}
		if got := ans.Entries("hosts"); len(got) != 0 {
			t.Errorf("entries = %v, want none", got)
		}
	})

	t.Run("--yes adds nothing", func(t *testing.T) {
		ans, err := Defaults{}.Ask(context.Background(), []Question{group})
		if err != nil {
			t.Fatal(err)
		}
		if got := ans.Entries("hosts"); len(got) != 0 {
			t.Errorf("entries = %v, want none", got)
		}
	})
}

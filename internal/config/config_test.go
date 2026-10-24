package config

import (
	"strings"
	"testing"

	"github.com/bitbynik/exon/internal/usererr"
)

var known = []string{"apps", "configs"}

type appsCfg struct {
	Casks []string `toml:"casks"`
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want []string // substrings of the error
	}{
		{"syntax error names the line", "[apps]\ncasks = [\"zed\"\nfoo = 1\n", []string{"line 3"}},
		{"unknown section", "[aps]\ncasks = []\n", []string{`unknown section "aps"`, "apps, configs"}},
		{"section must be a table", "apps = 1\n", []string{`"apps" must be a [section]`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse("exon.toml", []byte(tt.toml), known)
			if err == nil {
				t.Fatal("no error")
			}
			shown := err.Error()
			if ue, ok := usererr.As(err); ok {
				shown += "\n" + ue.Hint // what the user sees
			}
			for _, w := range tt.want {
				if !strings.Contains(shown, w) {
					t.Errorf("message %q does not contain %q", shown, w)
				}
			}
		})
	}
}

func TestSectionDecode(t *testing.T) {
	f, err := parse("exon.toml", []byte("[apps]\ncasks = [\"zed\"]\ncask = [\"typo\"]\n"), known)
	if err != nil {
		t.Fatal(err)
	}
	var c appsCfg
	err = f.Section("apps").Decode(&c)
	if err == nil || !strings.Contains(err.Error(), "unknown key cask") || !strings.Contains(err.Error(), "[apps]") {
		t.Errorf("err = %v, want unknown key error naming [apps] and cask", err)
	}

	var empty appsCfg
	if err := f.Section("configs").Decode(&empty); err != nil || empty.Casks != nil {
		t.Errorf("missing section: err = %v, value = %+v; want no error, zero value", err, empty)
	}
}

func TestSectionDecodeWrongType(t *testing.T) {
	f, err := parse("exon.toml", []byte("[apps]\ncasks = \"zed\"\n"), known)
	if err != nil {
		t.Fatal(err)
	}
	var c appsCfg
	if err := f.Section("apps").Decode(&c); err == nil || !strings.Contains(err.Error(), "[apps]") {
		t.Errorf("err = %v, want a type error naming [apps]", err)
	}
}

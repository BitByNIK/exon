package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "cfg")
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		arg     string
		want    Source
		wantErr bool
	}{
		{arg: "bitbynik/exon-config", want: Source{Owner: "bitbynik", Repo: "exon-config"}},
		{arg: "https://github.com/bitbynik/exon-config", want: Source{Owner: "bitbynik", Repo: "exon-config"}},
		{arg: "https://github.com/bitbynik/exon-config.git", want: Source{Owner: "bitbynik", Repo: "exon-config"}},
		{arg: "~/cfg", want: Source{Dir: local}},
		{arg: local, want: Source{Dir: local}},
		{arg: "~/missing", wantErr: true},
		{arg: "just-a-word", wantErr: true},
		{arg: "https://gitlab.com/a/b", wantErr: true},
		{arg: "a/b/c", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			got, err := Parse(tt.arg, home)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

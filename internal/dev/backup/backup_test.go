package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/dev/backup"
	"github.com/bitbynik/exon/internal/sys"
	"github.com/bitbynik/exon/internal/usererr"
)

type entry struct {
	name     string
	typeflag byte
	body     string
	link     string
}

func file(name, body string) entry { return entry{name: name, typeflag: tar.TypeReg, body: body} }

// write makes a .tar.gz with the given entries and returns its path.
func write(t *testing.T, entries ...entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "b.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: 0o600, Size: int64(len(e.body)), Linkname: e.link}
		if e.typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil && e.typeflag == tar.TypeReg {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectAnOrdinaryBackup(t *testing.T) {
	path := write(t,
		entry{name: "./", typeflag: tar.TypeDir},
		entry{name: "./state/", typeflag: tar.TypeDir},
		file("./state/gh/hosts.yml", "token"),
		file("./state/omp/agent/agent.db", "0123456789"),
		file("./shell/bashrc", "alias ll=ls"),
		// A link to an absolute path is ordinary data: omp skills can link to a notes folder.
		entry{name: "./state/omp/agent/skills/notes", typeflag: tar.TypeSymlink, link: "/mnt/notes/.skills/notes"},
	)
	info, err := backup.Inspect(sys.OSFS{}, path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Files != 3 || info.Bytes != int64(len("token")+10+len("alias ll=ls")) || len(info.Problems) != 0 {
		t.Errorf("info = %+v, want 3 files, their bytes and no problems", info)
	}
}

func TestInspectRefusesWhatCouldEscapeOrReplaceTheTools(t *testing.T) {
	tests := map[string][]entry{
		"absolute path":           {file("/etc/cron.d/evil", "x")},
		"parent folder":           {file("../outside", "x")},
		"parent hidden in a path": {file("state/../../outside", "x")},
		"the programs":            {file("./bin/omp", "x")},
		"the tool versions":       {file("versions/omp", "x")},
		"the container scripts":   {file("init", "x")},
		"through a link": {
			entry{name: "state/x", typeflag: tar.TypeSymlink, link: "/etc"},
			file("state/x/passwd", "x"),
		},
		"a hard link out of the volume": {entry{name: "state/h", typeflag: tar.TypeLink, link: "/etc/passwd"}},
		"a device":                      {entry{name: "state/dev", typeflag: tar.TypeChar}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			info, err := backup.Inspect(sys.OSFS{}, write(t, append([]entry{file("state/ok", "fine")}, entries...)...))
			if err != nil {
				t.Fatal(err)
			}
			if len(info.Problems) == 0 {
				t.Errorf("info = %+v, want the entry named as a problem", info)
			}
		})
	}
}

func TestInspectFilesThatAreNotBackups(t *testing.T) {
	notGzip := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notGzip, []byte("just text"), 0o644); err != nil {
		t.Fatal(err)
	}
	gzipNotTar := filepath.Join(t.TempDir(), "x.gz")
	f, _ := os.Create(gzipNotTar)
	gz := gzip.NewWriter(f)
	_, _ = gz.Write([]byte(strings.Repeat("not a tar archive ", 100)))
	_ = gz.Close()
	_ = f.Close()

	tests := map[string]struct{ path, msg string }{
		"plain text":   {notGzip, "is not a toolbox backup"},
		"gzip, no tar": {gzipNotTar, "is not a toolbox backup"},
		"empty":        {write(t), "has no files"},
		"missing":      {filepath.Join(t.TempDir(), "nope"), "cannot read"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := backup.Inspect(sys.OSFS{}, tt.path)
			if ue, ok := usererr.As(err); !ok || !strings.Contains(ue.Msg, tt.msg) {
				t.Errorf("err = %v, want a user error saying %q", err, tt.msg)
			}
		})
	}
}

func TestNames(t *testing.T) {
	when := time.Date(2026, 10, 25, 10, 55, 3, 0, time.UTC)
	if got := backup.FileName(when); got != "toolbox-2026-10-25_105503.tar.gz" {
		t.Errorf("FileName = %s", got)
	}
	if got := backup.DefaultDir("/Users/ng"); got != "/Users/ng/Backups/exon" {
		t.Errorf("DefaultDir = %s", got)
	}
}

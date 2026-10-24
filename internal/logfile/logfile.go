// Package logfile creates one log file per run and removes old ones.
package logfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Stamp is the timestamp format used for log and backup names. It sorts by time.
const Stamp = "2006-01-02_150405"

// Open creates dir/<stamp>.log and deletes all but the newest keep logs.
func Open(dir string, now time.Time, keep int) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, now.Format(Stamp)+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	prune(dir, keep)
	return f, nil
}

func prune(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var logs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			logs = append(logs, e.Name())
		}
	}
	slices.Sort(logs)
	for len(logs) > keep {
		_ = os.Remove(filepath.Join(dir, logs[0]))
		logs = logs[1:]
	}
}

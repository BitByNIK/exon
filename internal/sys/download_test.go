package sys_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitbynik/exon/internal/sys"
)

// testData is size bytes of repeatable noise, so a wrongly placed piece is noticed.
func testData(size int) []byte {
	r := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(r.IntN(256))
	}
	return b
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func newDownloader() sys.OSDownloader {
	return sys.OSDownloader{Parts: 4, RetryBase: time.Millisecond}
}

func download(f sys.Fetch) error {
	return newDownloader().Download(context.Background(), f)
}

// serveRanges answers "bytes=a-b" requests like a real file server. fail, if not nil,
// may return true to cut the connection after half of the requested bytes.
func serveRanges(data []byte, fail func(start int64) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a, b int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &b); err != nil {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		b = min(b, int64(len(data))-1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, len(data)))
		w.Header().Set("Content-Length", fmt.Sprint(b-a+1))
		w.WriteHeader(http.StatusPartialContent)
		if fail != nil && fail(a) {
			_, _ = w.Write(data[a : a+(b-a+1)/2])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write(data[a : b+1])
	})
}

func readBack(t *testing.T, path string) []byte {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("leftover partial file %s", e.Name())
		}
	}
}

func TestDownloadInPieces(t *testing.T) {
	data := testData(20 << 20)
	var requests atomic.Int32
	h := serveRanges(data, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "sub", "file")
	var mu sync.Mutex
	var last, total int64
	err := download(sys.Fetch{URL: srv.URL, Dest: dest, Progress: func(done, tot int64) {
		mu.Lock()
		defer mu.Unlock()
		if done < last {
			t.Errorf("progress went backwards: %d after %d", done, last)
		}
		last, total = done, tot
	}})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(readBack(t, dest), data) {
		t.Error("downloaded content differs from the source")
	}
	if got := requests.Load(); got != 5 { // one probe plus four pieces
		t.Errorf("requests = %d, want 5 (probe + 4 pieces)", got)
	}
	if last != int64(len(data)) || total != int64(len(data)) {
		t.Errorf("final progress = %d of %d, want %d of %d", last, total, len(data), len(data))
	}
	assertNoLeftovers(t, filepath.Dir(dest))
}

func TestDownloadSmallFileIsOnePiece(t *testing.T) {
	data := testData(1000)
	var requests atomic.Int32
	h := serveRanges(data, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "file")
	if err := download(sys.Fetch{URL: srv.URL, Dest: dest}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack(t, dest), data) {
		t.Error("downloaded content differs from the source")
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 (probe + one piece)", got)
	}
}

func TestDownloadFromServerWithoutRanges(t *testing.T) {
	data := testData(6 << 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data) // ignores Range: always the whole file with 200
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "file")
	var total int64
	err := download(sys.Fetch{URL: srv.URL, Dest: dest, Progress: func(_, tot int64) { total = tot }})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack(t, dest), data) {
		t.Error("downloaded content differs from the source")
	}
	if total != int64(len(data)) {
		t.Errorf("reported total = %d, want %d", total, len(data))
	}
}

func TestDownloadResumesAFailedPiece(t *testing.T) {
	data := testData(20 << 20)
	var mu sync.Mutex
	cut := map[int64]bool{}
	var starts []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &a)
		mu.Lock()
		starts = append(starts, a)
		mu.Unlock()
		serveRanges(data, func(start int64) bool {
			mu.Lock()
			defer mu.Unlock()
			first := start > 0 && !cut[start] // cut every piece except the probe once
			cut[start] = true
			return first && start%(5<<20) == 0
		}).ServeHTTP(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "file")
	if err := download(sys.Fetch{URL: srv.URL, Dest: dest}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack(t, dest), data) {
		t.Error("content differs after resuming")
	}
	resumed := 0
	for _, s := range starts {
		if s > 0 && s%(5<<20) != 0 {
			resumed++ // a request that starts inside a piece is a resume
		}
	}
	if resumed == 0 {
		t.Errorf("no resumed request seen; range starts were %v", starts)
	}
}

func TestDownloadNotFoundIsNotRetried(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "file")
	err := download(sys.Fetch{URL: srv.URL, Dest: dest})
	if se, ok := errors.AsType[*sys.StatusError](err); !ok || se.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want a StatusError with code 404", err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("dest exists after failure: %v", statErr)
	}
}

func TestDownloadGivesUpAndCleansUp(t *testing.T) {
	data := testData(10 << 20)
	h := serveRanges(data, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			h.ServeHTTP(w, r) // the probe works; every piece then fails
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "file")
	if err := download(sys.Fetch{URL: srv.URL, Dest: dest}); err == nil {
		t.Fatal("want an error when every piece fails")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dest exists after failure: %v", err)
	}
	assertNoLeftovers(t, dir)
}

func TestDownloadFollowsRedirects(t *testing.T) {
	data := testData(1000)
	mux := http.NewServeMux()
	mux.Handle("/real", serveRanges(data, nil))
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/real", http.StatusFound) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "file")
	if err := download(sys.Fetch{URL: srv.URL + "/start", Dest: dest}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack(t, dest), data) {
		t.Error("downloaded content differs from the source")
	}
}

func TestDownloadChecksum(t *testing.T) {
	data := testData(20 << 20)
	srv := httptest.NewServer(serveRanges(data, nil))
	defer srv.Close()

	t.Run("matching checksum", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "file")
		if err := download(sys.Fetch{URL: srv.URL, Dest: dest, SHA256: strings.ToUpper(sum(data))}); err != nil {
			t.Fatalf("a correct checksum (any letter case) must pass: %v", err)
		}
	})

	t.Run("wrong checksum keeps the old file", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "file")
		if err := os.WriteFile(dest, []byte("previous version"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := download(sys.Fetch{URL: srv.URL, Dest: dest, SHA256: sum([]byte("something else"))})
		if !errors.Is(err, sys.ErrChecksum) {
			t.Fatalf("err = %v, want ErrChecksum", err)
		}
		if got := readBack(t, dest); string(got) != "previous version" {
			t.Errorf("a bad download replaced the existing file with %d bytes", len(got))
		}
		assertNoLeftovers(t, dir)
	})

	t.Run("no checksum means no check", func(t *testing.T) {
		if err := download(sys.Fetch{URL: srv.URL, Dest: filepath.Join(t.TempDir(), "file")}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"a":1}`)) })
	mux.HandleFunc("/limited", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "slow down", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d := newDownloader()
	body, err := d.Get(context.Background(), srv.URL+"/ok")
	if err != nil || string(body) != `{"a":1}` {
		t.Errorf("Get = %q, %v", body, err)
	}

	_, err = d.Get(context.Background(), srv.URL+"/limited")
	se, ok := errors.AsType[*sys.StatusError](err)
	if !ok || se.Code != http.StatusForbidden || se.Header.Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("err = %v, want a StatusError that carries the answer's headers", err)
	}
}

func TestReadOnlyDownloaderBlocksDownloadsOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer srv.Close()

	ro := sys.ReadOnlyDownloader(newDownloader())
	err := ro.Download(context.Background(), sys.Fetch{URL: srv.URL, Dest: filepath.Join(t.TempDir(), "f")})
	if !errors.Is(err, sys.ErrReadOnly) {
		t.Errorf("Download err = %v, want ErrReadOnly", err)
	}
	if _, err := ro.Get(context.Background(), srv.URL); err != nil {
		t.Errorf("Get should still work while planning: %v", err)
	}
}

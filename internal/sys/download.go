package sys

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Downloader fetches files over HTTPS.
type Downloader interface {
	// Get returns the body of a small answer, such as GitHub's release data.
	// It changes nothing, so planning may use it.
	Get(ctx context.Context, url string) ([]byte, error)
	// Download saves a file, replacing any existing one only once the whole
	// download has arrived and passed its checksum.
	Download(ctx context.Context, f Fetch) error
}

// Fetch describes one download.
type Fetch struct {
	URL  string
	Dest string
	// SHA256 is the expected checksum, as lowercase hex. Empty means the
	// download is not checked.
	SHA256 string
	// Progress, if not nil, is called as bytes arrive and once at the end;
	// total is -1 when the size is not known.
	Progress func(done, total int64)
}

// ErrChecksum means a download arrived complete but is not the file that was expected.
var ErrChecksum = errors.New("checksum mismatch")

// StatusError is an unexpected HTTP answer.
type StatusError struct {
	URL    string
	Code   int
	Header http.Header
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.URL, e.Code) }

// DefaultParts is how many pieces of one file are fetched at the same time.
// GitHub's download servers limit each connection, not the line, so several
// connections are several times faster for big files.
const DefaultParts = 8

const (
	minPiece      = 4 << 20 // smaller files are not split
	pieceAttempts = 4
	reportEvery   = 100 * time.Millisecond
	maxGetBody    = 8 << 20
)

// OSDownloader is the real Downloader, built on net/http.
type OSDownloader struct {
	// Client is used for every request; nil means a client with no timeout.
	Client *http.Client
	// Parts is how many pieces are fetched at once; 0 means DefaultParts.
	Parts int
	// RetryBase is the first wait before retrying a failed piece; 0 means one second.
	RetryBase time.Duration
}

func (d OSDownloader) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{}
}

func (d OSDownloader) parts() int {
	if d.Parts > 0 {
		return d.Parts
	}
	return DefaultParts
}

func (d OSDownloader) retryBase() time.Duration {
	if d.RetryBase > 0 {
		return d.RetryBase
	}
	return time.Second
}

// Get implements Downloader.
func (d OSDownloader) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{URL: url, Code: resp.StatusCode, Header: resp.Header}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxGetBody))
}

// Download implements Downloader.
func (d OSDownloader) Download(ctx context.Context, f Fetch) error {
	t, err := d.probe(ctx, f.URL)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(f.Dest), 0o755); err != nil {
		return err
	}
	part := f.Dest + ".part"
	if err = d.fetchTo(ctx, part, t, f.Progress); err == nil && f.SHA256 != "" {
		err = verifySHA256(part, f.SHA256)
	}
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, f.Dest)
}

// verifySHA256 checks the file at path against a hex checksum.
func verifySHA256(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("%w: got %s, want %s", ErrChecksum, got, want)
	}
	return nil
}

// fetchTo downloads t into a new file at path and closes it.
func (d OSDownloader) fetchTo(ctx context.Context, path string, t target, progress func(done, total int64)) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	m := &meter{total: t.size, report: progress}
	if t.ranged {
		err = d.fetchPieces(ctx, f, t, m)
	} else {
		err = d.fetchWhole(ctx, f, t, m)
	}
	if err == nil {
		m.finish()
	}
	return err
}

// target is what probe learned about a download.
type target struct {
	url    string // the final address, after redirects
	size   int64  // -1 if unknown
	ranged bool   // the server can send a piece of the file
}

// probe asks for the first byte to learn the size and whether pieces work.
func (d OSDownloader) probe(ctx context.Context, url string) (target, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return target{}, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := d.client().Do(req)
	if err != nil {
		return target{}, err
	}
	defer resp.Body.Close()
	final := resp.Request.URL.String()
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if size, ok := rangeTotal(resp.Header.Get("Content-Range")); ok {
			return target{url: final, size: size, ranged: true}, nil
		}
		return target{}, fmt.Errorf("%s: unreadable Content-Range %q", url, resp.Header.Get("Content-Range"))
	case http.StatusOK:
		return target{url: final, size: resp.ContentLength}, nil
	default:
		return target{}, &StatusError{URL: url, Code: resp.StatusCode, Header: resp.Header}
	}
}

// rangeTotal reads N from a "bytes 0-0/N" header.
func rangeTotal(h string) (int64, bool) {
	_, total, ok := strings.Cut(h, "/")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(total, 10, 64)
	return n, err == nil && n > 0
}

// fetchPieces downloads the file as several ranges at once.
func (d OSDownloader) fetchPieces(ctx context.Context, f *os.File, t target, m *meter) error {
	n := int(min(int64(d.parts()), (t.size+minPiece-1)/minPiece))
	n = max(n, 1)
	if err := f.Truncate(t.size); err != nil {
		return err
	}
	per := (t.size + int64(n) - 1) / int64(n)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, n)
	for i := range n {
		start := int64(i) * per
		end := min(start+per, t.size) - 1
		go func() { errs <- d.fetchRange(ctx, f, t.url, start, end, m) }()
	}
	var first error
	for range n {
		if err := <-errs; err != nil && first == nil {
			first = err
			cancel() // stop the other pieces; their errors are not interesting
		}
	}
	return first
}

// fetchRange downloads bytes start..end, resuming after a failure from where it stopped.
func (d OSDownloader) fetchRange(ctx context.Context, f *os.File, url string, start, end int64, m *meter) error {
	pos := start
	return Retry(ctx, pieceAttempts, d.retryBase(), retryableDownload, func() error {
		n, err := d.copyRange(ctx, f, url, pos, end, m)
		pos += n
		return err
	})
}

func (d OSDownloader) copyRange(ctx context.Context, f *os.File, url string, pos, end int64, m *meter) (int64, error) {
	if pos > end {
		return 0, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", pos, end))
	resp, err := d.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, &StatusError{URL: url, Code: resp.StatusCode, Header: resp.Header}
	}
	want := end - pos + 1
	n, err := io.Copy(&countWriter{w: io.NewOffsetWriter(f, pos), m: m}, io.LimitReader(resp.Body, want))
	if err == nil && n < want {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

// fetchWhole downloads a file from a server that cannot send pieces, as one stream.
func (d OSDownloader) fetchWhole(ctx context.Context, f *os.File, t target, m *meter) error {
	return Retry(ctx, pieceAttempts, d.retryBase(), retryableDownload, func() error {
		if err := f.Truncate(0); err != nil {
			return err
		}
		m.reset()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
		if err != nil {
			return err
		}
		resp, err := d.client().Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return &StatusError{URL: t.url, Code: resp.StatusCode, Header: resp.Header}
		}
		n, err := io.Copy(&countWriter{w: io.NewOffsetWriter(f, 0), m: m}, resp.Body)
		if err == nil && t.size >= 0 && n != t.size {
			err = io.ErrUnexpectedEOF
		}
		return err
	})
}

// retryableDownload says whether a failed attempt is worth repeating: network
// trouble and server errors are; "not found" and the like are not.
func retryableDownload(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if se, ok := errors.AsType[*StatusError](err); ok {
		return se.Code >= 500 || se.Code == http.StatusTooManyRequests
	}
	return true
}

// countWriter reports every write to the meter.
type countWriter struct {
	w io.Writer
	m *meter
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.m.add(int64(n))
	return n, err
}

// meter counts downloaded bytes and reports them, at most every reportEvery,
// from any goroutine.
type meter struct {
	total  int64
	report func(done, total int64)

	mu   sync.Mutex
	done int64
	last time.Time
}

func (m *meter) add(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done += n
	if m.report != nil && time.Since(m.last) >= reportEvery {
		m.last = time.Now()
		m.report(m.done, m.total)
	}
}

func (m *meter) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done = 0
}

// finish reports the final count, whatever the throttle says.
func (m *meter) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.report != nil {
		m.report(m.done, m.total)
	}
}

// ReadOnlyDownloader returns a Downloader whose Download always fails with ErrReadOnly.
func ReadOnlyDownloader(d Downloader) Downloader { return readOnlyDownloader{d} }

type readOnlyDownloader struct{ Downloader }

func (readOnlyDownloader) Download(_ context.Context, f Fetch) error {
	return fmt.Errorf("%w: download %s", ErrReadOnly, f.URL)
}

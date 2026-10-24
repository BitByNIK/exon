package systest

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/bitbynik/exon/internal/sys"
)

// FakeDownloader is a sys.Downloader that answers from maps, with no network.
type FakeDownloader struct {
	// Files maps an address to the bytes Get returns and Download saves;
	// other addresses fail with 404.
	Files map[string][]byte
	// Errs maps an address to the error every request for it fails with.
	Errs map[string]error

	mu      sync.Mutex
	fetches []sys.Fetch
}

var _ sys.Downloader = (*FakeDownloader)(nil)

// Get answers from Errs and Files.
func (f *FakeDownloader) Get(_ context.Context, url string) ([]byte, error) {
	return f.lookup(url)
}

// Download records the request and writes the bytes from Files to its Dest.
// It does not check the checksum; tests assert on Fetches instead.
func (f *FakeDownloader) Download(_ context.Context, req sys.Fetch) error {
	f.mu.Lock()
	f.fetches = append(f.fetches, req)
	f.mu.Unlock()
	body, err := f.lookup(req.URL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(req.Dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(req.Dest, body, 0o644); err != nil {
		return err
	}
	if req.Progress != nil {
		req.Progress(int64(len(body)), int64(len(body)))
	}
	return nil
}

func (f *FakeDownloader) lookup(url string) ([]byte, error) {
	if err, ok := f.Errs[url]; ok {
		return nil, err
	}
	if body, ok := f.Files[url]; ok {
		return body, nil
	}
	return nil, &sys.StatusError{URL: url, Code: 404}
}

// Fetches returns the requests passed to Download so far.
func (f *FakeDownloader) Fetches() []sys.Fetch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sys.Fetch(nil), f.fetches...)
}

package archive

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/openpgp"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/deb"
)

type pool struct {
	ch chan bool
}

// newPool constructs a pool which can be used by up to n workers at
// the same time.
func newPool(n int) *pool {
	return &pool{
		ch: make(chan bool, n),
	}
}

func (p *pool) lock() {
	p.ch <- true
}

func (p *pool) unlock() {
	<-p.ch
}

// Downloader makes files from the Debian archive available.
type Downloader struct {
	// Parallel limits the maximum number of concurrent archive accesses.
	Parallel int

	// MaxTransientRetries caps retries of transient errors.
	// The default value of 0 means retry forever.
	MaxTransientRetries int

	// Mirror is the HTTP URL of a Debian mirror, e.g. "https://deb.debian.org/debian".
	// Mirror supports TLS and HTTP/2.
	Mirror string

	// LocalMirror overrides Mirror with a local file system path.
	// E.g. /srv/mirrors/debian on DSA-maintained machines.
	LocalMirror string

	// TempDir is passed as dir argument to ioutil.TempFile.
	// The default value of empty string uses the default directory, see os.TempDir.
	TempDir string

	once    sync.Once
	pool    *pool
	keyring openpgp.EntityList
}

type transientError struct {
	error
}

// open returns an io.ReadCloser for reading fn from the archive, and fns last
// modification time.
func (g *Downloader) open(fn string) (io.ReadCloser, time.Time, error) {
	if g.LocalMirror != "" {
		f, err := os.Open(filepath.Join(g.LocalMirror, fn))
		if err != nil {
			return nil, time.Time{}, err
		}
		fi, err := f.Stat()
		if err != nil {
			return nil, time.Time{}, err
		}
		return f, fi.ModTime(), nil
	}
	u := strings.TrimSuffix(g.Mirror, "/") + "/" + fn
	resp, err := http.Get(u)
	if err != nil {
		return nil, time.Time{}, transientError{err}
	}
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		err := fmt.Errorf("download(%s): unexpected HTTP status code: got %d, want %d", u, got, want)
		// Not entirely accurate or exhaustive, but HTTP 5xx is generally
		// transient.
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			return nil, time.Time{}, transientError{err}
		}
		return nil, time.Time{}, err
	}
	modTime, err := http.ParseTime(resp.Header.Get("Last-Modified"))
	if err != nil {
		return nil, time.Time{}, err
	}
	return resp.Body, modTime, nil
}

func (g *Downloader) tempFileWithFilename(verifier io.WriteCloser, decompressor deb.DecompressorFunc, fn string) (*os.File, error) {
	g.pool.lock()
	defer g.pool.unlock()

	f, err := ioutil.TempFile(g.TempDir, "archive-")
	if err != nil {
		return nil, err
	}

	var (
		r       io.ReadCloser
		modTime time.Time
	)
	for retry := 0; ; retry++ {
		var err error
		r, modTime, err = g.open(fn)
		if err == nil {
			break
		}
		if te, ok := err.(transientError); ok && retry < g.MaxTransientRetries {
			log.Printf("transient error %v, retrying (attempt %d of %d)", te, retry, g.MaxTransientRetries)
			continue
		}
		os.Remove(f.Name())
		f.Close()
		return nil, err
	}
	defer r.Close()

	if _, err := f.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}

	rd, err := decompressor(io.TeeReader(r, verifier))
	if err != nil {
		return nil, err
	}

	w := bufio.NewWriter(f)

	if _, err := io.Copy(w, rd); err != nil {
		return nil, err
	}

	if err := verifier.Close(); err != nil {
		return nil, err
	}

	if err := w.Flush(); err != nil {
		return nil, err
	}

	if _, err := f.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}

	if err := os.Chtimes(f.Name(), modTime, modTime); err != nil {
		return nil, err
	}

	return f, nil
}

// TempFile calls ioutil.TempFile, then downloads fh from the archive and
// returns it.
//
// If hash checksum verification fails, the temporary file will be deleted and
// an error will be returned.
//
// If err is nil, the caller must remove the file when no longer needed:
//    f, err := r.GetTempFile(fh)
//    if err != nil {
//        return nil, err
//    }
//    defer f.Close() // avoid leaking resources
//    defer os.Remove(f.Name()) // remove from file system
//    return parseSources(f)
//
// Remember that files must be closed before they can be read by external processes:
//    f, err := r.GetTempFile(fh)
//    if err != nil {
//        return err
//    }
//    if err := f.Close(); err != nil {
//        return err
//    }
//    defer os.Remove(f.Name()) // remove from file system
//    return exec.Command("tar", "xf", f.Name()).Run()
func (g *Downloader) TempFile(fh control.FileHash) (*os.File, error) {
	if err := g.init(); err != nil {
		return nil, err
	}

	verifier, err := fh.Verifier()
	if err != nil {
		return nil, err
	}
	decompressor := deb.DecompressorFor(filepath.Ext(fh.Filename))
	return g.tempFileWithFilename(verifier, decompressor, fh.Filename)
}

func (g *Downloader) init() error {
	var err error
	g.once.Do(func() {
		g.pool = newPool(g.Parallel)
		err = g.loadArchiveKeyrings()
	})
	return err
}

// DebianArchiveKeyring is the full path to the GPG keyring containing the
// public keys used for signing the Debian archive.
const DebianArchiveKeyring = "/usr/share/keyrings/debian-archive-keyring.gpg"

// loadArchiveKeyrings loads the debian-archive-keyring.gpg keyring
// shipped in the debian-archive-keyring Debian package (NOT all
// trusted keys stored in /etc/apt/trusted.gpg.d).
func (g *Downloader) loadArchiveKeyrings() error {
	f, err := os.Open(DebianArchiveKeyring)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s not found. On Debian, install the debian-archive-keyring package.", DebianArchiveKeyring)
		}
		return err
	}
	defer f.Close()
	g.keyring, err = openpgp.ReadKeyRing(f)
	return err
}

// ReleaseDownloader is like Downloader, but for a specific release
// (e.g. unstable).
type ReleaseDownloader struct {
	// LastModified contains the last modification timestamp of the release
	// metadata file.
	LastModified time.Time

	acquireByHash bool
	g             *Downloader
	suite         string
}

// GetTempFile is like Downloader.GetTempFile, but for fhs of the release.
func (r *ReleaseDownloader) TempFile(fh control.FileHash) (*os.File, error) {
	fn := "dists/" + r.suite + "/" + fh.Filename
	if r.acquireByHash {
		fn = fh.ByHashPath(fn)
	}
	verifier, err := fh.Verifier()
	if err != nil {
		return nil, err
	}
	decompressor := deb.DecompressorFor(filepath.Ext(fh.Filename))
	return r.g.tempFileWithFilename(verifier, decompressor, fn)
}

type noopVerifier struct{}

func (*noopVerifier) Write([]byte) (int, error) { return 0, nil }
func (*noopVerifier) Close() error              { return nil }

// Release returns a release and a corresponding ReleaseDownloader from the archive.
//
// If cryptographic verification using DebianArchiveKeyring fails, an error will
// be returned.
func (g *Downloader) Release(suite string) (*Release, *ReleaseDownloader, error) {
	if err := g.init(); err != nil {
		return nil, nil, err
	}

	u := "dists/" + suite + "/InRelease"
	if strings.HasSuffix(suite, "stable") {
		// Only testing (buster) has InRelease at this point, so fall back to
		// Release for *stable:
		u = "dists/" + suite + "/Release"
	}
	verifier := &noopVerifier{}             // verification happens in LoadInRelease
	decompressor := deb.DecompressorFor("") // InRelease is not compressed
	f, err := g.tempFileWithFilename(verifier, decompressor, u)
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	r, err := LoadInRelease(f, &g.keyring)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadInRelease(%s): %v", u, err)
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}

	return r, &ReleaseDownloader{fi.ModTime(), r.AcquireByHash, g, suite}, nil
}

// DefaultDownloader is a ready-to-use Downloader, used by convenience wrappers
// such as CachedRelease and, by extension, TempFile.
var DefaultDownloader = &Downloader{
	Parallel:            10,
	MaxTransientRetries: 3,
	Mirror:              "https://deb.debian.org/debian",
}

type cachedRelease struct {
	r   *Release
	rd  *ReleaseDownloader
	err error
}

var (
	releaseCacheMu sync.Mutex
	releaseCache   = make(map[string]cachedRelease)
)

// CachedRelease returns DefaultDownloader.Release(suite), caching releases for
// the duration of the process.
func CachedRelease(suite string) (*Release, *ReleaseDownloader, error) {
	releaseCacheMu.Lock()
	cached, ok := releaseCache[suite]
	releaseCacheMu.Unlock()
	if ok {
		return cached.r, cached.rd, cached.err
	}

	r, rd, err := DefaultDownloader.Release(suite)
	releaseCacheMu.Lock()
	defer releaseCacheMu.Unlock()
	if cached, ok := releaseCache[suite]; ok {
		// Another goroutine raced us, return cached values for consistency:
		return cached.r, cached.rd, cached.err
	}

	releaseCache[suite] = cachedRelease{r, rd, err}
	return r, rd, err
}

// TempFile expects a path starting with dists/<suite>, calls Release(suite),
// looks up the remaining path within the release and calls TempFile on the
// corresponding ReleaseDownloader.
func TempFile(path string) (*os.File, error) {
	if !strings.HasPrefix(path, "dists/") {
		return nil, fmt.Errorf("path %q does not start with dists/", path)
	}
	path = strings.TrimPrefix(path, "dists/")
	suite := strings.Split(path, "/")[0]
	r, rd, err := CachedRelease(suite)
	if err != nil {
		return nil, err
	}
	remainder := strings.TrimPrefix(path, suite+"/")
	fhs, ok := r.Indices()[remainder]
	if !ok {
		return nil, fmt.Errorf("%s not found", remainder)
	}
	return rd.TempFile(fhs[0])
}

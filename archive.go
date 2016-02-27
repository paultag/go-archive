package archive

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"compress/bzip2"
	"compress/gzip"
	"xi2.org/x/xz"

	"golang.org/x/crypto/openpgp"
	"pault.ag/go/debian/dependency"
)

// Common Types {{{

type Closer func() error
type PathReader func(path string) (io.Reader, Closer, error)

// known compression types {{{

type compressionReader func(io.Reader) (io.Reader, error)

func gzipNewReader(r io.Reader) (io.Reader, error) {
	return gzip.NewReader(r)
}

func xzNewReader(r io.Reader) (io.Reader, error) {
	return xz.NewReader(r, 0)
}

func bzipNewReader(r io.Reader) (io.Reader, error) {
	return bzip2.NewReader(r), nil
}

var knownCompressionAlgorithms = map[string]compressionReader{
	".gz":  gzipNewReader,
	".bz2": bzipNewReader,
	".xz":  xzNewReader,
}

// }}}

// }}}

// Archive {{{

type Archive struct {
	root       string
	pathReader PathReader
	keyring    *openpgp.EntityList
}

func (a Archive) getFile(requestPath string) (io.Reader, Closer, error) {
	archivePath := path.Join(a.root, requestPath)
	reader, closer, err := a.pathReader(archivePath)
	if err != nil {
		return nil, nil, err
	}

	for suffix, decompressor := range knownCompressionAlgorithms {
		if strings.HasSuffix(requestPath, suffix) {
			newReader, err := decompressor(reader)
			if err != nil {
				closer()
				return nil, nil, err
			}
			return newReader, closer, nil
		}
	}

	return reader, closer, err
}

// Release {{{

func (a Archive) Release(suite string) (*Release, error) {
	/* We'll just read the InRelease, results in the fewest IO calls */
	inReleasePath := path.Join("dists", suite, "InRelease")
	reader, closer, err := a.getFile(inReleasePath)
	if err != nil {
		return nil, err
	}

	/* We don't need to return the Closer, since the entire reader will
	 * be consumed -- and no remaining data is given to the user */
	defer closer()

	release, err := LoadInRelease(reader, a.keyring)
	if err != nil {
		return nil, err
	}
	return release, err
}

// }}}

// Packages {{{

func (a Archive) Packages(suite, component string, arch dependency.Arch) (*Packages, Closer, error) {
	packagesPath := path.Join(
		"dists", suite, component,
		fmt.Sprintf("binary-%s", arch.String()),
		"Packages",
	)

	reader, closer, err := a.getFile(packagesPath)
	if err != nil {
		return nil, nil, err
	}

	/* Packages isn't signed */
	release, err := LoadPackages(reader)
	if err != nil {
		closer()
		return nil, nil, err
	}
	return release, closer, err
}

// }}}

// Sources {{{

func (a Archive) Sources(suite, component string, arch dependency.Arch) (*Sources, Closer, error) {
	packagesPath := path.Join("dists", suite, component, "source", "Sources.gz")

	reader, closer, err := a.getFile(packagesPath)
	if err != nil {
		return nil, nil, err
	}

	/* Packages isn't signed */
	release, err := LoadSources(reader)
	if err != nil {
		closer()
		return nil, nil, err
	}
	return release, closer, err
}

// }}}

// Constructors {{{

func NewArchive(
	root string,
	pathReader PathReader,
	keyring *openpgp.EntityList,
) Archive {
	return Archive{
		root:       root,
		pathReader: pathReader,
		keyring:    keyring,
	}
}

func NewFilesystemArchive(root string, keyring *openpgp.EntityList) Archive {
	return NewArchive(root, filesystemPathReader, keyring)
}

// }}}

// Readers {{{

func filesystemPathReader(path string) (io.Reader, Closer, error) {
	fd, err := os.Open(path)
	return fd, fd.Close, err
}

// }}}

// }}}

// vim: foldmethod=marker

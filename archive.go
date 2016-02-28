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

func (a Archive) Suite(name string) (*Suite, error) {
	inReleasePath := path.Join("dists", name, "InRelease")
	reader, closer, err := a.getFile(inReleasePath)
	if err != nil {
		return nil, err
	}
	defer closer()

	release, err := LoadInRelease(reader, a.keyring)
	if err != nil {
		return nil, err
	}
	return &Suite{
		Release: *release,
		archive: a,
	}, nil
}

type Suite struct {
	Release Release
	archive Archive
}

func (s Suite) HasArch(arch dependency.Arch) bool {
	for _, el := range s.Release.Architectures {
		if el.Is(&arch) {
			return true
		}
	}
	return false
}

func (s Suite) HasComponent(component string) bool {
	for _, el := range s.Release.Components {
		if el == component {
			return true
		}
	}
	return false
}

func (s Suite) Sources(component string) (*Sources, Closer, error) {
	if !s.HasComponent(component) {
		return nil, nil, fmt.Errorf("No such component: '%s'", component)
	}
	sourcesPath := path.Join(
		"dists", s.Release.Suite, component, "source", "Sources",
	)
	reader, closer, err := s.archive.getFile(sourcesPath)
	if err != nil {
		return nil, nil, err
	}
	sources, err := LoadSources(reader)
	if err != nil {
		closer()
		return nil, nil, err
	}
	return sources, closer, nil
}

func (s Suite) Packages(component string, arch dependency.Arch) (*Packages, Closer, error) {
	if !s.HasComponent(component) {
		return nil, nil, fmt.Errorf("No such component: '%s'", component)
	}
	if !s.HasArch(arch) {
		return nil, nil, fmt.Errorf("No such arch: '%s'", arch.String())
	}
	packagesPath := path.Join(
		"dists", s.Release.Suite, component,
		fmt.Sprintf("binary-%s", arch.String()),
		"Packages",
	)
	reader, closer, err := s.archive.getFile(packagesPath)
	if err != nil {
		return nil, nil, err
	}
	packages, err := LoadPackages(reader)
	if err != nil {
		closer()
		return nil, nil, err
	}
	return packages, closer, nil
}

// getFile wrapper {{{

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

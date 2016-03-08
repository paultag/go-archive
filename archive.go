package archive

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"compress/bzip2"
	"compress/gzip"
	"xi2.org/x/xz"

	"golang.org/x/crypto/openpgp"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
)

type PathReader func(path string) (io.Reader, Closer, error)

// Archive {{{

type Archive struct {
	root       string
	pathReader PathReader
	keyring    *openpgp.EntityList
}

// Get an archive Suite for a given Archive {{{

func (a Archive) Suite(name string) (*Suite, error) {
	inReleasePath := path.Join("dists", name, "InRelease")
	reader, closer, err := a.getFile(inReleasePath, nil)
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

// }}}

// Suite {{{

// Suite struct {{{

type Suite struct {
	Release Release
	archive Archive
}

// }}}

// HasArch {{{

func (s Suite) HasArch(arch dependency.Arch) bool {
	for _, el := range s.Release.Architectures {
		if el.Is(&arch) {
			return true
		}
	}
	return false
}

// }}}

// HasComponent {{{

func (s Suite) HasComponent(component string) bool {
	for _, el := range s.Release.Components {
		if el == component {
			return true
		}
	}
	return false
}

// }}}

// getHashedFileReader - get a Hashed File {{{

func (s Suite) getHashedFileReader(suitePath string) (io.Reader, Closer, control.FileHashValidators, error) {
	hashes := s.Release.Indices()[suitePath]
	if len(hashes) == 0 {
		return nil, nil, nil, fmt.Errorf("Undeclared file in InRelease")
	}

	validators, err := hashes.Validators()
	if err != nil {
		return nil, nil, nil, err
	}

	packagesPath := path.Join("dists", s.Release.Suite, suitePath)
	reader, closer, err := s.archive.getFile(packagesPath, validators.Writer())

	if err != nil {
		return nil, nil, nil, err
	}

	return reader, closer, validators, err
}

// }}}

// Packages index for a Suite {{{

func (s Suite) Packages(component string, arch dependency.Arch) ([]Package, error) {
	ret := []Package{}

	if !s.HasComponent(component) {
		return ret, fmt.Errorf("No such component: '%s'", component)
	}
	if !s.HasArch(arch) {
		return ret, fmt.Errorf("No such arch: '%s'", arch.String())
	}
	suitePath := path.Join(
		component,
		fmt.Sprintf("binary-%s", arch.String()),
		"Packages",
	)

	validationReader, closer, validators, err := s.getHashedFileReader(suitePath)
	if err != nil {
		return []Package{}, err
	}
	defer closer()

	packages, err := LoadPackages(validationReader)
	if err != nil {
		return []Package{}, err
	}

	for {
		next, err := packages.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return []Package{}, err
		}
		ret = append(ret, *next)
	}

	if !validators.Validate() {
		return nil, fmt.Errorf("Index hashes don't match!")
	}

	return ret, nil
}

// }}}

// Sources index for a Suite {{{

func (s Suite) Sources(component string) ([]Source, error) {
	ret := []Source{}

	if !s.HasComponent(component) {
		return ret, fmt.Errorf("No such component: '%s'", component)
	}
	suitePath := path.Join(component, "source", "Sources.gz")

	validationReader, closer, validators, err := s.getHashedFileReader(suitePath)
	if err != nil {
		return []Source{}, err
	}
	defer closer()

	packages, err := LoadSources(validationReader)
	if err != nil {
		return []Source{}, err
	}

	for {
		next, err := packages.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return []Source{}, err
		}
		ret = append(ret, *next)
	}

	if !validators.Validate() {
		return nil, fmt.Errorf("Index hashes don't match!")
	}

	return ret, nil
}

// }}}

// }}}

// getFile wrapper {{{

func (a Archive) getFile(requestPath string, tee io.Writer) (io.Reader, Closer, error) {
	archivePath := a.root + "/" + requestPath
	reader, closer, err := a.pathReader(archivePath)
	if err != nil {
		return nil, nil, err
	}

	reader, err := compression.Decompress(reader, archivePath, tee)
	if err != nil {
		closer()
	}
	return reader, err

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

// Filesystem Archive {{{

func NewFilesystemArchive(root string, keyring *openpgp.EntityList) Archive {
	return NewArchive(root, filesystemPathReader, keyring)
}

func NewHttpArchive(root string, keyring *openpgp.EntityList) Archive {
	return NewArchive(root, httpPathReader, keyring)
}

// }}}

// }}}

// Readers {{{

func filesystemPathReader(path string) (io.Reader, Closer, error) {
	fd, err := os.Open(path)
	return fd, fd.Close, err
}

func httpPathReader(uri string) (io.Reader, Closer, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return nil, nil, err
	}
	return resp.Body, resp.Body.Close, nil
}

// }}}

// }}}

// vim: foldmethod=marker

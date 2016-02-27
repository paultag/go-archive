package archive

import (
	"io"
	"os"
	"path"

	"golang.org/x/crypto/openpgp"
)

// Common Types {{{

type Closer func() error
type PathReader func(path string) (io.Reader, Closer, error)

// }}}

// Archive {{{

type Archive struct {
	root       string
	pathReader PathReader
	keyring    *openpgp.EntityList
}

// Release {{{

func (a Archive) Release(suite string) (*Release, error) {
	/* We'll just read the InRelease, results in the fewest IO calls */
	inReleasePath := path.Join(a.root, "dists", suite, "InRelease")
	reader, closer, err := a.pathReader(inReleasePath)
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

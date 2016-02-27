package archive

import (
	"fmt"
	"os"
	"path"

	"golang.org/x/crypto/openpgp"
	"pault.ag/go/debian/dependency"
)

// archive LoadRelease  name
// archive LoadSources  component
// archive LoadPackages arches, components

type Closer func() error

type ArchiveReader interface {
	GetRelease(name string) (*Release, error)
	GetSources(component string) (*Sources, Closer, error)
	GetPackages(suite, component string, arch dependency.Arch) (*Packages, Closer, error)
}

type filesystemArchiveReader struct {
	root    string
	keyring *openpgp.EntityList
}

func (f filesystemArchiveReader) GetRelease(name string) (*Release, error) {
	inReleasePath := path.Join(f.root, "dists", name, "InRelease")
	return LoadInReleaseFile(inReleasePath, f.keyring)
}

func (f filesystemArchiveReader) GetSources(component string) (*Sources, Closer, error) {
	return nil, nil, nil
}

func (f filesystemArchiveReader) GetPackages(suite, component string, arch dependency.Arch) (*Packages, Closer, error) {
	packagesPath := path.Join(
		f.root, "dists", suite, component,
		fmt.Sprintf("binary-%s", arch.String()),
		"Packages",
	)
	fd, err := os.Open(packagesPath)
	if err != nil {
		return nil, nil, err
	}

	packages, err := LoadPackages(fd)
	if err != nil {
		fd.Close()
		return nil, nil, err
	}
	return packages, fd.Close, nil
}

func NewFilesystemArchiveReader(
	root string,
	keyring *openpgp.EntityList,
) (ArchiveReader, error) {
	return filesystemArchiveReader{root: root, keyring: keyring}, nil
}

package archive

import (
	"fmt"
	"io"
	"os"
	"path"

	"pault.ag/go/blobstore"
	"pault.ag/go/debian/deb"
)

type Pool struct {
	store blobstore.Store
	suite *Suite
}

func poolPrefix(source string) string {
	return path.Join(source[0:1], source)
}

func (p Pool) Include(debFile *deb.Deb) (string, error) {
	fd, err := os.Open(debFile.Path)
	if err != nil {
		return "", err
	}

	writer, err := p.store.Create()
	if err != nil {
		return "", err
	}
	defer writer.Close()

	_, err = io.Copy(writer, fd)
	if err != nil {
		return "", err
	}

	obj, err := p.store.Commit(*writer)
	debPath := path.Join(
		"pool",
		poolPrefix(debFile.Control.SourceName()),
		fmt.Sprintf(
			"%s_%s_%s.deb",
			debFile.Control.Package,
			debFile.Control.Version,
			debFile.Control.Architecture,
		),
	)

	return debPath, p.store.Link(*obj, debPath)
}

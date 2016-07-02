package archive

import (
	"fmt"
	"io"
	"os"
	"path"

	"pault.ag/go/blobstore"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/deb"
)

type Pool struct {
	Store blobstore.Store
}

func poolPrefix(source string) string {
	return path.Join(source[0:1], source)
}

func (p Pool) Copy(path string) (*blobstore.Object, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	writer, err := p.Store.Create()
	if err != nil {
		return nil, err
	}
	defer writer.Close()

	if _, err := io.Copy(writer, fd); err != nil {
		return nil, err
	}

	obj, err := p.Store.Commit(*writer)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

func (p Pool) IncludeSources(dsc *control.DSC) (string, map[string]blobstore.Object, error) {
	files := map[string]blobstore.Object{}

	targetDir := path.Join("pool", poolPrefix(dsc.Source))

	for _, file := range dsc.Files {
		obj, err := p.Copy(file.Filename)
		if err != nil {
			return "", nil, err
		}

		localName := path.Base(file.Filename)
		files[path.Join(targetDir, localName)] = *obj
	}

	obj, err := p.Copy(dsc.Filename)
	if err != nil {
		return "", nil, err
	}

	localName := path.Base(dsc.Filename)
	files[path.Join(targetDir, localName)] = *obj

	for path, object := range files {
		if err := p.Store.Link(object, path); err != nil {
			return "", nil, err
		}
	}

	return targetDir, files, nil
}

func (p Pool) IncludeDeb(debFile *deb.Deb) (string, *blobstore.Object, error) {
	obj, err := p.Copy(debFile.Path)
	if err != nil {
		return "", nil, err
	}

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

	return debPath, obj, p.Store.Link(*obj, debPath)
}

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
	store blobstore.Store
	suite *Suite
}

func poolPrefix(source string) string {
	return path.Join(source[0:1], source)
}

func (p Pool) Copy(path string) (*blobstore.Object, []control.FileHash, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, []control.FileHash{}, nil
	}
	defer fd.Close()

	hasherWriter, hashers, err := getHashers(p.suite)
	if err != nil {
		return nil, []control.FileHash{}, nil
	}

	writer, err := p.store.Create()
	if err != nil {
		return nil, []control.FileHash{}, nil
	}
	defer writer.Close()

	targetWriter := io.MultiWriter(hasherWriter, writer)
	if _, err := io.Copy(targetWriter, fd); err != nil {
		return nil, []control.FileHash{}, nil
	}

	obj, err := p.store.Commit(*writer)
	if err != nil {
		return nil, []control.FileHash{}, nil
	}

	fileHashes := []control.FileHash{}
	for _, hasher := range hashers {
		fileHash := control.FileHashFromHasher(path, *hasher)
		fileHashes = append(fileHashes, fileHash)
	}

	return obj, fileHashes, nil
}

func (p Pool) IncludeSources(dsc *control.DSC) (string, map[string]blobstore.Object, error) {
	files := map[string]blobstore.Object{}

	targetDir := path.Join("pool", poolPrefix(dsc.Source))

	for _, file := range dsc.Files {
		obj, _, err := p.Copy(file.Filename)
		if err != nil {
			return "", nil, err
		}

		localName := path.Base(file.Filename)
		files[path.Join(targetDir, localName)] = *obj
	}

	obj, _, err := p.Copy(dsc.Filename)
	if err != nil {
		return "", nil, err
	}

	localName := path.Base(dsc.Filename)
	files[path.Join(targetDir, localName)] = *obj

	for path, object := range files {
		if err := p.store.Link(object, path); err != nil {
			return "", nil, err
		}
	}

	return targetDir, files, nil
}

func (p Pool) IncludeDeb(debFile *deb.Deb) (string, *blobstore.Object, error) {
	obj, _, err := p.Copy(debFile.Path)
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

	return debPath, obj, p.store.Link(*obj, debPath)
}

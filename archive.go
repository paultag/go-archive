package archive

import (
	"fmt"
	"io"
	"path"

	"pault.ag/go/blobstore"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/transput"
)

// New {{{

func New(path string) (*Archive, error) {
	store, err := blobstore.Load(path)
	if err != nil {
		return nil, err
	}
	return &Archive{
		store: *store,
	}, nil
}

// }}}

// Archive magic {{{

type Archive struct {
	store blobstore.Store
}

func (a Archive) Suite(name string) (*Suite, error) {
	/* Get the Release / InRelease */
	inRelease := Release{}
	components := map[string]*Component{}

	fd, err := a.store.OpenPath(path.Join("dists", name, "InRelease"))
	if err == nil {
		defer fd.Close()
		if err := control.Unmarshal(&inRelease, fd); err != nil {
			return nil, err
		}

		for _, name := range inRelease.Components {
			components[name] = &Component{Packages: []Package{}}
		}
	}

	suite := Suite{
		Name: name,

		release:    inRelease,
		Components: components,
	}

	suite.features.Hashes = []string{"sha256", "sha1"}

	return &suite, nil
}

func (a Archive) encode(suite Suite, path string, data interface{}) (*blobstore.Object, []control.FileHash, error) {
	writer, err := a.store.Create()
	if err != nil {
		return nil, nil, err
	}
	defer writer.Close()

	hashers := []*transput.Hasher{}
	writers := []io.Writer{writer}

	for _, algorithm := range suite.features.Hashes {
		hasher, err := transput.NewHasher(algorithm)
		if err != nil {
			return nil, nil, err
		}
		hashers = append(hashers, hasher)
		writers = append(writers, hasher)
	}

	multiWriter := io.MultiWriter(writers...)

	encoder, err := control.NewEncoder(multiWriter)
	if err != nil {
		return nil, nil, err
	}

	if err := encoder.Encode(data); err != nil {
		return nil, nil, err
	}

	obj, err := a.store.Commit(*writer)
	if err != nil {
		return nil, nil, err
	}

	fileHashs := []control.FileHash{}
	for _, hasher := range hashers {
		fileHashs = append(fileHashs, control.FileHashFromHasher(path, *hasher))
	}

	return obj, fileHashs, nil
}

func (a Archive) Engross(suite Suite) (map[string]blobstore.Object, error) {
	files := map[string]blobstore.Object{}

	release := Release{
		Description:   "",
		Origin:        "",
		Label:         "",
		Version:       "",
		Suite:         suite.Name,
		Codename:      "",
		Components:    suite.ComponenetNames(),
		Architectures: suite.Arches(),
		SHA256:        []control.SHA256FileHash{},
		SHA1:          []control.SHA1FileHash{},
		SHA512:        []control.SHA512FileHash{},
		MD5Sum:        []control.MD5FileHash{},
	}

	for name, component := range suite.Components {
		for arch, pkgs := range component.ByArch() {
			filePath := path.Join("dists", suite.Name, name,
				fmt.Sprintf("binary-%s", arch), "Packages")

			obj, hashes, err := a.encode(suite, filePath, pkgs)
			if err != nil {
				return nil, err
			}

			for _, hash := range hashes {
				if err := release.AddHash(hash); err != nil {
					return nil, err
				}
			}

			files[filePath] = *obj
		}
	}

	filePath := path.Join("dists", suite.Name, "Release")
	obj, _, err := a.encode(suite, filePath, release)
	if err != nil {
		return nil, err
	}
	files[filePath] = *obj

	return files, nil
}

func (a Archive) Link(blobs map[string]blobstore.Object) error {
	for p, obj := range blobs {
		if err := a.store.Link(obj, p); err != nil {
			return err
		}
	}
	return nil
}

// }}}

// Suite magic {{{

type Suite struct {
	Name string

	release    Release
	Components map[string]*Component

	features struct {
		Hashes []string
	}
}

func (s Suite) Arches() []dependency.Arch {
	ret := map[dependency.Arch]bool{}
	for _, component := range s.Components {
		for _, arch := range component.Arches() {
			ret[arch] = true
		}
	}
	r := []dependency.Arch{}
	for arch, _ := range ret {
		r = append(r, arch)
	}
	return r
}

func (s Suite) ComponenetNames() []string {
	ret := []string{}
	for name, _ := range s.Components {
		ret = append(ret, name)
	}
	return ret
}

func (s Suite) Add(name string, pkg Package) {
	if _, ok := s.Components[name]; !ok {
		s.Components[name] = &Component{Packages: []Package{}}
	}
	s.Components[name].Add(pkg)
}

// }}}

// Component magic {{{

type Component struct {
	Packages []Package
}

func (c *Component) ByArch() map[dependency.Arch][]Package {
	ret := map[dependency.Arch][]Package{}

	for _, pkg := range c.Packages {
		packages := ret[pkg.Architecture]
		ret[pkg.Architecture] = append(packages, pkg)
	}

	return ret
}

func (c *Component) Arches() []dependency.Arch {
	ret := []dependency.Arch{}
	for _, pkg := range c.Packages {
		ret = append(ret, pkg.Architecture)
	}
	return ret
}

func (c *Component) Add(p Package) {
	c.Packages = append(c.Packages, p)
}

// }}}

// vim: foldmethod=marker

package archive

import (
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"golang.org/x/crypto/openpgp"

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

	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	return &Archive{
		store: *store,
	}, nil
}

func NewWritable(path string, keyring string, keyid uint64) (*Archive, error) {
	archive, err := New(path)
	if err != nil {
		return nil, err
	}

	fd, err := os.Open(keyring)
	if err != nil {
		return nil, err
	}

	el, err := openpgp.ReadKeyRing(fd)
	if err != nil {
		return nil, err
	}

	keys := el.KeysById(keyid)

	if len(keys) == 0 {
		return nil, fmt.Errorf("No keys matched that key ID")
	}

	if len(keys) != 1 {
		return nil, fmt.Errorf("Too many keys matched that key ID")
	}

	archive.signingKey = keys[0].Entity

	return archive, err
}

// }}}

// Archive magic {{{

type Archive struct {
	store      blobstore.Store
	signingKey *openpgp.Entity
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

func (a Archive) encodeHashedBySuite(path string, suite Suite, data interface{}) (*blobstore.Object, []control.FileHash, error) {

	hashers := []*transput.Hasher{}
	for _, algorithm := range suite.features.Hashes {
		hasher, err := transput.NewHasher(algorithm)
		if err != nil {
			return nil, nil, err
		}
		hashers = append(hashers, hasher)
	}

	return a.encodeHashed(path, hashers, data)
}

func (a Archive) encodeHashed(path string, hashers []*transput.Hasher, data interface{}) (*blobstore.Object, []control.FileHash, error) {

	writers := []io.Writer{}
	for _, hasher := range hashers {
		writers = append(writers, hasher)
	}

	obj, err := a.encode(data, io.MultiWriter(writers...))
	if err != nil {
		return nil, nil, err
	}

	fileHashs := []control.FileHash{}
	for _, hasher := range hashers {
		fileHashs = append(fileHashs, control.FileHashFromHasher(path, *hasher))
	}

	return obj, fileHashs, nil
}

func (a Archive) encode(data interface{}, tee io.Writer) (*blobstore.Object, error) {
	writer, err := a.store.Create()
	if err != nil {
		return nil, err
	}
	defer writer.Close()

	var target io.Writer = writer
	if tee != nil {
		target = io.MultiWriter(writer, tee)
	}

	encoder, err := control.NewEncoder(target)
	if err != nil {
		return nil, err
	}

	if err := encoder.Encode(data); err != nil {
		return nil, err
	}

	obj, err := a.store.Commit(*writer)
	if err != nil {
		return nil, err
	}

	return obj, nil
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
		Date:          time.Now().Format(time.RFC1123Z),
		SHA256:        []control.SHA256FileHash{},
		SHA1:          []control.SHA1FileHash{},
		SHA512:        []control.SHA512FileHash{},
		MD5Sum:        []control.MD5FileHash{},
	}

	for name, component := range suite.Components {
		for arch, pkgs := range component.ByArch() {
			filePath := path.Join("dists", suite.Name, name,
				fmt.Sprintf("binary-%s", arch), "Packages")

			obj, hashes, err := a.encodeHashedBySuite(filePath, suite, pkgs)
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
	obj, sig, err := a.encodeSigned(release)
	if err != nil {
		return nil, err
	}
	files[filePath] = *obj
	files[fmt.Sprintf("%s.gpg", filePath)] = *sig

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

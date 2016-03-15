package archive

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"

	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/transput"
)

func NewHashers(suite Suite, target io.Writer) (io.Writer, []*transput.Hasher, error) {
	return transput.NewHasherWriters(suite.features.Hashes, target)
}

// Archive {{{

type Archive struct {
	root string
}

func NewArchive(root string) Archive {
	return Archive{root: root}
}

func (a Archive) Suite(name string) (*Suite, error) {
	inRelease := path.Join(a.root, "dists", name, "InRelease")
	suite := Suite{Binaries: map[string]Binaries{}}

	/* Feature flags */
	suite.features.Hashes = []string{"sha256", "sha1", "md5"}

	fd, err := os.Open(inRelease)
	if err != nil {
		return nil, err
	}

	defer fd.Close()
	return &suite, control.Unmarshal(&suite, fd)
}

func (a Archive) linkObject(suite Suite, hash *transput.Hasher, targetPath string) error {
	objPath := path.Join("by-hash", hash.Name(), fmt.Sprintf("%x", hash.Sum(nil)))
	_, err := os.Readlink(targetPath)

	if err == nil {
		if err := os.Remove(targetPath); err != nil {
			return err
		}
	}

	if err := os.Symlink(objPath, targetPath); err != nil {
		return err
	}
	return nil
}

func (a Archive) objectPath(suite Suite, hash *transput.Hasher) (string, string) {
	dirPath := path.Join(a.root, "dists", suite.Suite, "by-hash", hash.Name())
	objPath := path.Join(dirPath, fmt.Sprintf("%x", hash.Sum(nil)))
	return dirPath, objPath
}

func (a Archive) objectWriteCloserFor(suite Suite, hash *transput.Hasher) (io.WriteCloser, error) {
	dirPath, objPath := a.objectPath(suite, hash)

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, err
	}

	fd, err := os.Create(objPath)
	return fd, err
}

func (a Archive) writeObject(suite Suite, data io.Reader) ([]*transput.Hasher, error) {
	target := bytes.Buffer{}
	writer, hashers, err := NewHashers(suite, &target)
	if err != nil {
		return nil, err
	}
	/* So, we have a set of hashers, and the target Buffer */

	c, err := io.Copy(writer, data)
	if err != nil {
		return nil, err
	}

	for _, hash := range hashers {
		if c != hash.Size() {
			return nil, fmt.Errorf("Size mismatch: %s (%d), wrote %d",
				hash.Name(), hash.Size(), c)
		}
	}

	writers := []io.Writer{}
	for _, hash := range hashers {
		fd, err := a.objectWriteCloserFor(suite, hash)
		if err != nil {
			return nil, err
		}
		defer fd.Close()
		writers = append(writers, fd)
	}

	_, err = io.Copy(io.MultiWriter(writers...), &target)
	if err != nil {
		return nil, err
	}

	return hashers, nil
}

func (a Archive) Engross(suite Suite) (*Release, error) {
	engrossedFiles := map[string][]*transput.Hasher{}

	for component, packages := range suite.Binaries {
		for _, arch := range packages.Arches() {
			filePath := path.Join(
				component,
				fmt.Sprintf("binary-%s", arch),
				"Packages",
			)

			target := bytes.Buffer{}
			if err := packages.WriteArchTo(arch, &target); err != nil {
				return nil, err
			}

			hashers, err := a.writeObject(suite, &target)
			if err != nil {
				return nil, err
			}
			engrossedFiles[filePath] = hashers
		}
	}

	ret := Release{
		Description:   suite.Description,
		Origin:        suite.Origin,
		Label:         suite.Label,
		Version:       suite.Version,
		Suite:         suite.Suite,
		Codename:      suite.Codename,
		Components:    suite.Components(),
		Architectures: suite.Arches(),
		MD5Sum:        []control.MD5FileHash{},
		SHA1:          []control.SHA1FileHash{},
		SHA256:        []control.SHA256FileHash{},
	}

	for path, hashers := range engrossedFiles {
		for _, hasher := range hashers {
			fileHash := control.FileHashFromHasher(path, *hasher)
			switch fileHash.Algorithm {
			case "sha1":
				ret.SHA1 = append(ret.SHA1, control.SHA1FileHash{fileHash})
			case "sha256":
				ret.SHA256 = append(ret.SHA256, control.SHA256FileHash{fileHash})
			case "md5":
				ret.MD5Sum = append(ret.MD5Sum, control.MD5FileHash{fileHash})
			default:
				return nil, fmt.Errorf("Unknown hash algorithm: '%s'", fileHash.Algorithm)
			}
		}
	}

	target := bytes.Buffer{}
	if err := control.Marshal(&target, &ret); err != nil {
		return nil, err
	}

	hashers, err := a.writeObject(suite, &target)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(a.root, "dists", suite.Suite, "Release")

	if err := a.linkObject(suite, hashers[0], filePath); err != nil {
		return nil, err
	}

	return &ret, nil
}

// }}}

// Suite {{{

type Suite struct {
	control.Paragraph

	Description string
	Origin      string
	Label       string
	Version     string
	Suite       string `required:"true"`
	Codename    string

	Binaries map[string]Binaries

	features struct {
		Hashes []string
		/* Compressors ... */
	}
}

func (a Suite) Arches() []dependency.Arch {
	arches := map[dependency.Arch]bool{}

	for _, binaries := range a.Binaries {
		for _, arch := range binaries.Arches() {
			arches[arch] = true
		}
	}

	seen := []dependency.Arch{}
	for arch, _ := range arches {
		seen = append(seen, arch)
	}
	return seen
}

func (s Suite) Components() []string {
	components := []string{}
	for component, _ := range s.Binaries {
		components = append(components, component)
	}
	return components
}

func (s Suite) AddPackageTo(component string, pkg Package) {
	if _, ok := s.Binaries[component]; !ok {
		s.Binaries[component] = Binaries{
			arches: map[dependency.Arch][]Package{},
		}
	}
	s.Binaries[component].Add(pkg)
}

// }}}

// Binaries {{{

type Binaries struct {
	arches map[dependency.Arch][]Package
}

func (b Binaries) Add(pkg Package) {
	arch := pkg.Architecture
	b.arches[arch] = append(b.arches[arch], pkg)
}

func (b Binaries) Get(arch dependency.Arch) []Package {
	return b.arches[arch]
}

func (b Binaries) Arches() []dependency.Arch {
	ret := []dependency.Arch{}
	for arch, _ := range b.arches {
		ret = append(ret, arch)
	}
	return ret
}

func (b Binaries) Has(arch dependency.Arch) bool {
	_, ok := b.arches[arch]
	return ok
}

func (b Binaries) WriteArchTo(arch dependency.Arch, out io.Writer) error {
	encoder, err := control.NewEncoder(out)
	if err != nil {
		return err
	}
	if packages, ok := b.arches[arch]; ok {
		for _, pkg := range packages {
			if err := encoder.Encode(pkg); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("No such arch: '%s'", arch)
	}
	return nil
}

// }}}

// vim: foldmethod=marker

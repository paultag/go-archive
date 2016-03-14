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

func (a Archive) writeObject(suite Suite, data io.Reader, hashes []*transput.Hasher) error {
	writers := []io.Writer{}

	for _, hash := range hashes {
		/* dists/<release>/by-hash/<algorithm>/<hash> */
		dirPath := path.Join(a.root, "dists", suite.Suite, "by-hash", hash.Name())

		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return err
		}

		fd, err := os.Create(path.Join(dirPath, fmt.Sprintf("%x", hash.Sum(nil))))
		if err != nil {
			return err
		}
		defer fd.Close()
		writers = append(writers, fd)
	}

	c, err := io.Copy(io.MultiWriter(writers...), data)
	if err != nil {
		return err
	}

	for _, hash := range hashes {
		if c != hash.Size() {
			return fmt.Errorf(
				"Size mismatch: %s (%d), wrote %d",
				hash.Name(), hash.Size(), c,
			)
		}
	}

	return nil
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
			writer, hashers, err := NewHashers(suite, &target)
			if err != nil {
				return nil, err
			}

			if err := packages.WriteArchTo(arch, writer); err != nil {
				return nil, err
			}

			if err := a.writeObject(suite, &target, hashers); err != nil {
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
			arches: map[string][]Package{},
		}
	}
	s.Binaries[component].Add(pkg)
}

// }}}

// Binaries {{{

type Binaries struct {
	arches map[string][]Package
}

func (b Binaries) Add(pkg Package) {
	arch := pkg.Architecture.String()
	b.arches[arch] = append(b.arches[arch], pkg)
}

func (b Binaries) Get(arch dependency.Arch) []Package {
	return b.arches[arch.String()]
}

func (b Binaries) Arches() []dependency.Arch {
	ret := []dependency.Arch{}

	for archName, _ := range b.arches {
		arch, err := dependency.ParseArch(archName)
		if err != nil {
			/* XXX: Wat */
			continue
		}
		ret = append(ret, *arch)
	}
	return ret
}

func (b Binaries) Has(arch dependency.Arch) bool {
	_, ok := b.arches[arch.String()]
	return ok
}

func (b Binaries) WriteArchTo(arch dependency.Arch, out io.Writer) error {
	encoder, err := control.NewEncoder(out)
	if err != nil {
		return err
	}
	if packages, ok := b.arches[arch.String()]; ok {
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

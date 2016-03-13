package archive

import (
	"os"
	"path"

	"pault.ag/go/debian/control"
)

var hashAlgorithms = []string{"md5", "sha256", "sha512"}
var compressAlgorithms = []string{"gz", ""}

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

	fd, err := os.Open(inRelease)
	if err != nil {
		return nil, err
	}

	defer fd.Close()
	return &suite, control.Unmarshal(&suite, fd)
}

// }}}

// Suite {{{

type Suite struct {
	control.Paragraph

	Description string
	Origin      string
	Label       string
	Version     string
	Suite       string
	Codename    string

	Binaries map[string]Binaries
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
		s.Binaries[component] = Binaries{arches: map[string][]Package{}}
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

// }}}

// vim: foldmethod=marker

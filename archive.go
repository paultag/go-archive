package archive

import (
	"os"
	"path"

	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
)

type Archive struct {
	root string
}

func NewArchive(root string) Archive {
	return Archive{root: root}
}

func (a Archive) Suite(name string) (*Suite, error) {
	inRelease := path.Join(a.root, "dists", name, "InRelease")
	suite := Suite{
		Packages: map[string][]Package{},
	}

	fd, err := os.Open(inRelease)
	if err != nil {
		return nil, err
	}

	defer fd.Close()
	return &suite, control.Unmarshal(&suite, fd)
}

type Suite struct {
	control.Paragraph

	Description string
	Origin      string
	Label       string
	Version     string
	Suite       string
	Codename    string

	Packages map[string][]Package
}

func (s Suite) Components() []string {
	components := []string{}
	for component, _ := range s.Packages {
		components = append(components, component)
	}
	return components
}

func (s Suite) Architectures() []dependency.Arch {
	arches := map[string]dependency.Arch{}

	for _, packages := range s.Packages {
		for _, pkg := range packages {
			arches[pkg.Architecture.String()] = pkg.Architecture
		}
	}

	ret := []dependency.Arch{}

	for _, el := range arches {
		ret = append(ret, el)
	}

	return ret
}

func (s Suite) AddPackageTo(component string, pkg Package) {
	if els, ok := s.Packages[component]; ok {
		s.Packages[component] = append(els, pkg)
	} else {
		s.Packages[component] = []Package{pkg}
	}
}

// vim: foldmethod=marker

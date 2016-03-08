package archive

import (
	"bytes"
	"fmt"
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
	suite := Suite{componentEncoders: map[string]*encoderTarget{}}

	fd, err := os.Open(inRelease)
	if err != nil {
		return nil, err
	}

	defer fd.Close()
	return &suite, control.Unmarshal(&suite, fd)
}

func (a Archive) Engross(suite Suite) error {
	for _, target := range suite.componentEncoders {
		fmt.Printf("%s", target.Holder.String())
	}
	return nil
}

type Suite struct {
	control.Paragraph

	Description   string
	Origin        string
	Label         string
	Version       string
	Suite         string
	Codename      string
	Components    []string `delim:" "`
	Architectures []dependency.Arch

	componentEncoders map[string]*encoderTarget
}

type encoderTarget struct {
	Encoder *control.Encoder
	Holder  *bytes.Buffer
}

func (s Suite) HasComponent(component string) bool {
	for _, el := range s.Components {
		if component == el {
			return true
		}
	}
	return false
}

func (s Suite) getEncoder(component string) (*encoderTarget, error) {
	if encoder, ok := s.componentEncoders[component]; ok {
		return encoder, nil
	}

	if s.HasComponent(component) {
		target := encoderTarget{
			Encoder: nil,
			Holder:  &bytes.Buffer{},
			/* XXX: Add Hashers */
		}
		encoder, err := control.NewEncoder(target.Holder)
		if err != nil {
			return nil, err
		}
		target.Encoder = encoder
		s.componentEncoders[component] = &target
		return &target, nil
	} else {
		return nil, fmt.Errorf("No such component: '%s'", component)
	}

}

func (s Suite) AddPackageTo(component string, entry Package) error {
	encoder, err := s.getEncoder(component)
	if err != nil {
		return nil
	}
	return encoder.Encoder.Encode(entry)
}

// vim: foldmethod=marker

package archive

import (
	"golang.org/x/crypto/openpgp"

	"pault.ag/go/blobstore"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
)

// Create a new Archive at the given `root` on the filesystem, with the
// openpgp.Entity `signer` (an Entity which contains an OpenPGP Private
// Key).
//
// This interface is intended to *write* Archives, not *read* them. Extra
// steps must be taken to load an Archive over the network, and attention
// must be paid when handling the Cryptographic chain of trust.
func New(path string, signer *openpgp.Entity) (*Archive, error) {
	store, err := blobstore.Load(path)
	if err != nil {
		return nil, err
	}

	return &Archive{
		store:      *store,
		signingKey: signer,
	}, nil
}

// Core Archive abstrcation. This contains helpers to write out package files,
// as well as handles creating underlying abstractions (such as Suites).
type Archive struct {
	store      blobstore.Store
	signingKey *openpgp.Entity
}

// Get a Suite for a given Archive. Information will be loaded from the
// InRelease file (if it exists) into the Suite object.
//
// The Suite object contains neat abstractions such as Components, and a
// number of helpers to collection information across all components, such
// as the `Arches()` helper.
func (a Archive) Suite(name string) (*Suite, error) {
	/* Get the Release / InRelease */
	components := map[string]Component{}
	suite := Suite{
		Components: components,
		archive:    &a,
	}

	suite.Pool = Pool{store: a.store, suite: &suite}
	suite.features.Hashes = []string{"sha256", "sha1"}
	// suite.features.Duration = "24h"

	return &suite, nil
}

// Use the default backend to remove any unlinked files from the Blob store.
func (a Archive) Decruft() error {
	return a.store.GC(blobstore.DumbGarbageCollector{})
}

type Suite struct {
	control.Paragraph

	archive *Archive

	Name        string `control:"Suite"`
	Description string
	Origin      string
	Label       string
	Version     string

	Components map[string]Component `control:"-"`
	Pool       Pool                 `control:"-"`

	features struct {
		Hashes   []string
		Duration string
	} `control:"-"`
}

// For a Suite, iterate over all known Components, and return a list of
// unique architectures. Not all Components may have all these arches.
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

func (s Suite) Component(name string) (*Component, error) {
	if _, ok := s.Components[name]; !ok {
		c, err := newComponent(*s.archive)
		if err != nil {
			return nil, err
		}
		s.Components[name] = *c
	}
	el := s.Components[name]
	return &el, nil
}

// Return a list of unique component names.
func (s Suite) ComponenetNames() []string {
	ret := []string{}
	for name, _ := range s.Components {
		ret = append(ret, name)
	}
	return ret
}

func newComponent(archive Archive) (*Component, error) {
	writer, err := archive.store.Create()
	if err != nil {
		return nil, err
	}

	encoder, err := control.NewEncoder(writer)
	if err != nil {
		return nil, err
	}

	return &Component{
		encoder: encoder,
		writer:  writer,
		archive: &archive,
	}, nil
}

// Collection of Binary Components.
type Collections struct {
	Collections map[dependency.Arch]PackageCollection
}

// Component is a section of the Archive, which is a set of Binary packages
// that are provided to the end user. Debian has three major ones, `main`,
// `contrib` and `non-free`.
type PackageCollection struct {
	encoder *control.Encoder
	writer  *blobstore.Writer
	archive *Archive
}

// vim: foldmethod=marker

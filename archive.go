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
	suite := Suite{
		archive:            &a,
		PackageCollections: PackageCollections{},
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

	/* Componenet        ~= PackageCollections
	 * Compoenent + Arch ~= PackageCollection */
	PackageCollections map[string]PackageCollections `control:"-"`
	Pool               Pool                          `control:"-"`

	features struct {
		Hashes   []string
		Duration string
	} `control:"-"`
}

type PackageCollections map[dependency.Arch]PackageCollection

func (p PackageCollections) get(arch dependency.Arch) PackageCollection {
	if _, ok := p[arch]; !ok {
		p[arch] = PackageCollection{}
	}
	return p[arch]
}

func (p PackageCollections) Add(pkg Package) error {
	collection := p.get(pkg.Architecture)
	return collection.Add(pkg)
}

// Component is a section of the Archive, which is a set of Binary packages
// that are provided to the end user. Debian has three major ones, `main`,
// `contrib` and `non-free`.
type PackageCollection struct {
	encoder *control.Encoder
	writer  *blobstore.Writer
	archive *Archive
	/* XXX: Add a flag to ensure alpha sorted entries */
}

func (p PackageCollection) Add(pkg Package) error {
	return p.encoder.Encode(pkg)
}

// vim: foldmethod=marker

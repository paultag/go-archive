package archive

import (
	"fmt"
	"io"
	"path"
	"time"

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

func (a Archive) Suite(name string) (*Suite, error) {
	suite := Suite{
		Name:       name,
		archive:    &a,
		components: map[string]Component{},
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

func (a Archive) Link(blobs map[string]blobstore.Object) error {
	for path, obj := range blobs {
		if err := a.store.Link(obj, path); err != nil {
			return err
		}
	}
	return nil
}

func (suite Suite) newRelease() Release {
	when := time.Now()
	release := Release{
		Suite:       suite.Name,
		Description: suite.Description,
		Origin:      suite.Origin,
		Label:       suite.Label,
		Version:     suite.Version,
	}
	release.Date = when.Format(time.RFC1123Z)
	release.Architectures = []dependency.Arch{}
	release.Components = []string{}
	release.SHA256 = []control.SHA256FileHash{}
	release.SHA1 = []control.SHA1FileHash{}
	release.SHA512 = []control.SHA512FileHash{}
	release.MD5Sum = []control.MD5FileHash{}
	return release
}

//
func (a Archive) Engross(suite Suite) (map[string]blobstore.Object, error) {
	// release := suite.newRelease()

	files := map[string]blobstore.Object{}

	for name, component := range suite.components {
		for arch, writer := range component.packageWriters {

			suitePath := path.Join(name, fmt.Sprintf("binary-%s", arch),
				"Packages")

			obj, err := a.store.Commit(*writer.handle)
			if err != nil {
				return nil, err
			}

			filePath := path.Join("dists", suite.Name, suitePath)
			files[filePath] = *obj
		}
	}

	/* Now, let's do some magic */

	return files, nil
}

//
type Suite struct {
	control.Paragraph

	archive *Archive

	Name        string `control:"Suite"`
	Description string
	Origin      string
	Label       string
	Version     string

	Pool       Pool                 `control:"-"`
	components map[string]Component `control"-"`

	features struct {
		Hashes   []string
		Duration string
	} `control:"-"`
}

/////////////////////////////////////////////////////////

func (s Suite) Component(name string) (*Component, error) {
	if _, ok := s.components[name]; !ok {
		comp, err := newComponent(s.archive)
		if err != nil {
			return nil, err
		}
		s.components[name] = *comp
		return comp, nil
	}
	el := s.components[name]
	return &el, nil
}

func newComponent(archive *Archive) (*Component, error) {
	return &Component{
		archive:        archive,
		packageWriters: map[dependency.Arch]*PackageWriter{},
	}, nil
}

type Component struct {
	archive        *Archive
	packageWriters map[dependency.Arch]*PackageWriter
	// sourceWriter *SourceWriter
}

func (c *Component) getWriter(arch dependency.Arch) (*PackageWriter, error) {
	if _, ok := c.packageWriters[arch]; !ok {
		writer, err := newPackageWriter(c.archive)
		if err != nil {
			return nil, err
		}
		c.packageWriters[arch] = writer
	}
	return c.packageWriters[arch], nil
}

func (c *Component) AddPackage(pkg Package) error {
	writer, err := c.getWriter(pkg.Architecture)
	if err != nil {
		return err
	}
	return writer.Add(pkg)
}

type packageWriters map[dependency.Arch]*PackageWriter

func newPackageWriter(archive *Archive) (*PackageWriter, error) {

	/* So, a PackageWriter is the thing we use to create a Packages entry
	 * for a given suite/component/binary-arch */

	handle, err := archive.store.Create()
	if err != nil {
		return nil, err
	}

	encoder, err := control.NewEncoder(handle)
	if err != nil {
		handle.Close()
		return nil, err
	}

	return &PackageWriter{
		archive: archive,
		writer:  handle,
		closer:  handle.Close,
		encoder: encoder,
		handle:  handle,
	}, nil
}

type PackageWriter struct {
	archive *Archive

	handle  *blobstore.Writer
	writer  io.Writer
	closer  func() error
	encoder *control.Encoder
}

func (p PackageWriter) Add(pkg Package) error {
	return p.encoder.Encode(pkg)
}

// vim: foldmethod=marker

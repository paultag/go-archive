package archive

import (
	"fmt"
	"io"
	"path"
	"time"

	"crypto"
	"crypto/sha512"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/clearsign"
	"golang.org/x/crypto/openpgp/packet"

	"pault.ag/go/blobstore"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/transput"
)

// Archive {{{

// Core Archive abstrcation. This contains helpers to write out package files,
// as well as handles creating underlying abstractions (such as Suites).
type Archive struct {
	store      blobstore.Store
	signingKey *openpgp.Entity
}

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

// Use the default backend to remove any unlinked files from the Blob store.
//
// If files you care about are not linked onto the stage, they will be removed
// by the garbage collector. Decruft only when you're sure the stage has been
// set.
func (a Archive) Decruft() error {
	return a.store.GC(blobstore.DumbGarbageCollector{})
}

// Given a list of objects, link them to the keyed paths.
func (a Archive) Link(blobs map[string]blobstore.Object) error {
	for path, obj := range blobs {
		if err := a.store.Link(obj, path); err != nil {
			return err
		}
	}
	return nil
}

// Create a new Release object from a Suite, passing off the Name, Description
// and constructing the rest of the goodies.
//
// This will be an entirely empty object, without anything read off disk.
func newRelease(suite Suite) (*Release, error) {
	when := time.Now()

	var validUntil string = ""
	if suite.features.Duration != "" {
		duration, err := time.ParseDuration(suite.features.Duration)
		if err != nil {
			return nil, err
		}
		validUntil = when.Add(duration).Format(time.RFC1123Z)
	}

	release := Release{
		Suite:       suite.Name,
		Description: suite.Description,
		ValidUntil:  validUntil,
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
	return &release, nil
}

// Engross a Suite for signing and final commit into the blobstore. This
// will return handle(s) to the signed and ready Objects, fit for passage
// to Link.
//
// This will contain all the related Packages and Release files.
func (a Archive) Engross(suite Suite) (map[string]blobstore.Object, error) {
	release, err := newRelease(suite)
	if err != nil {
		return nil, err
	}

	files := map[string]blobstore.Object{}
	arches := map[dependency.Arch]bool{}

	for name, component := range suite.components {
		release.Components = append(release.Components, name)

		for arch, writer := range component.packageWriters {
			arches[arch] = true

			suitePath := path.Join(name, fmt.Sprintf("binary-%s", arch),
				"Packages")

			obj, err := a.store.Commit(*writer.handle)
			if err != nil {
				return nil, err
			}

			for _, hasher := range writer.hashers {
				fileHash := control.FileHashFromHasher(suitePath, *hasher)
				release.AddHash(fileHash)
			}

			filePath := path.Join("dists", suite.Name, suitePath)
			files[filePath] = *obj
		}
	}

	for arch, _ := range arches {
		release.Architectures = append(release.Architectures, arch)
	}

	/* Now, let's do some magic */

	obj, sig, err := suite.archive.encodeSigned(release)
	if err != nil {
		return nil, err
	}

	filePath := path.Join("dists", suite.Name, "Release")
	files[filePath] = *obj
	files[fmt.Sprintf("%s.gpg", filePath)] = *sig

	obj, err = suite.archive.encodeClearsigned(release)
	if err != nil {
		return nil, err
	}

	files[path.Join("dists", suite.Name, "InRelease")] = *obj

	return files, nil
}

// Given a control.Marshal'able object, encode it to the blobstore, while
// also clearsigning the data.
func (a Archive) encodeClearsigned(data interface{}) (*blobstore.Object, error) {

	if a.signingKey == nil {
		return nil, fmt.Errorf("No signing key loaded")
	}

	fd, err := a.store.Create()
	if err != nil {
		return nil, err
	}

	defer fd.Close()
	wc, err := clearsign.Encode(fd, a.signingKey.PrivateKey, nil)
	if err != nil {
		return nil, err
	}

	encoder, err := control.NewEncoder(wc)
	if err != nil {
		return nil, err
	}

	if err := encoder.Encode(data); err != nil {
		return nil, err
	}

	if err := wc.Close(); err != nil {
		return nil, err
	}

	return a.store.Commit(*fd)
}

// Given a control.Marshal'able object, encode it to the blobstore, while
// also doing a detached OpenPGP signature. The objects returned (in order)
// are data, commited to the blobstore, the signature for that object, commited
// to the blobstore, and any error(s), finally.
func (a Archive) encodeSigned(data interface{}) (*blobstore.Object, *blobstore.Object, error) {
	/* Right, so, the trick here is that we secretly call out to encode,
	 * but tap it with a pipe into the signing code */

	if a.signingKey == nil {
		return nil, nil, fmt.Errorf("No signing key loaded")
	}

	signature, err := a.store.Create()
	if err != nil {
		return nil, nil, err
	}
	defer signature.Close()

	hash := sha512.New()

	obj, err := a.encode(data, hash)
	if err != nil {
		return nil, nil, err
	}

	sig := new(packet.Signature)
	sig.SigType = packet.SigTypeBinary
	sig.PubKeyAlgo = a.signingKey.PrivateKey.PubKeyAlgo

	sig.Hash = crypto.SHA512

	sig.CreationTime = new(packet.Config).Now()
	sig.IssuerKeyId = &(a.signingKey.PrivateKey.KeyId)

	err = sig.Sign(hash, a.signingKey.PrivateKey, nil)
	if err != nil {
		return nil, nil, err
	}

	if err := sig.Serialize(signature); err != nil {
		return nil, nil, err
	}

	sigObj, err := a.store.Commit(*signature)
	if err != nil {
		return nil, nil, err
	}

	return obj, sigObj, nil

}

// Encode a given control.Marshal'able object into the Blobstore, and return
// a handle to its object.
//
// The optinal argument `tap` will be written to as the object gets sent into
// the blobstore. This may be useful if you wish to have a copy of the data
// going into the store.
func (a Archive) encode(data interface{}, tap io.Writer) (*blobstore.Object, error) {
	fd, err := a.store.Create()
	if err != nil {
		return nil, err
	}

	var writer io.Writer = fd
	if tap != nil {
		writer = io.MultiWriter(fd, tap)
	}

	encoder, err := control.NewEncoder(writer)
	if err != nil {
		return nil, err
	}

	if err := encoder.Encode(data); err != nil {
		return nil, err
	}

	return a.store.Commit(*fd)
}

// }}}

// Suite {{{

// Abstraction to handle writing data into a Suite. This is a write-only
// target, and is not intended to read a Release file.
//
// This contains no state read off disk, and is purely for writing to.
type Suite struct {
	control.Paragraph

	archive *Archive

	Name        string `control:"Suite"`
	Description string
	Origin      string
	Label       string
	Version     string

	Pool       Pool                 `control:"-"`
	components map[string]Component `control:"-"`

	features struct {
		Hashes   []string
		Duration string
	} `control:"-"`
}

// Get a handle to write a given Suite from an Archive.
// The suite will be entirely blank, and attributes will not be
// read from the existing files, if any.
func (a Archive) Suite(name string) (*Suite, error) {
	suite := Suite{
		Name:       name,
		archive:    &a,
		components: map[string]Component{},
	}

	suite.Pool = Pool{store: a.store, suite: &suite}
	suite.features.Hashes = []string{"sha256", "sha1", "sha512"}
	suite.features.Duration = "240h"

	return &suite, nil
}

// Get or create a Component for a given Suite. If no such Component
// has been created so far, this will create a new object, otherwise
// it will return the existing entry.
//
// This contains no state read off disk, and is purely for writing to.
func (s Suite) Component(name string) (*Component, error) {
	if _, ok := s.components[name]; !ok {
		comp, err := newComponent(&s)
		if err != nil {
			return nil, err
		}
		s.components[name] = *comp
		return comp, nil
	}
	el := s.components[name]
	return &el, nil
}

// }}}

// Component {{{

// Small wrapper to represent a Component of a Suite, which, at its core
// is simply a set of Indexes to be written to.
//
// This contains no state read off disk, and is purely for writing to.
type Component struct {
	suite          *Suite
	packageWriters map[dependency.Arch]*PackageWriter
	// sourceWriter *SourceWriter
}

// Create a new Component, configured for use.
func newComponent(suite *Suite) (*Component, error) {
	return &Component{
		suite:          suite,
		packageWriters: map[dependency.Arch]*PackageWriter{},
	}, nil
}

// Get a given PackageWriter for an arch, or create one if none exists.
func (c *Component) getWriter(arch dependency.Arch) (*PackageWriter, error) {
	if _, ok := c.packageWriters[arch]; !ok {
		writer, err := newPackageWriter(c.suite)
		if err != nil {
			return nil, err
		}
		c.packageWriters[arch] = writer
	}
	return c.packageWriters[arch], nil
}

// Add a given Package to a Package List. Under the hood, this will
// get or create a PackageWriter, and invoke the .Add method on the
// Package Writer.
func (c *Component) AddPackage(pkg Package) error {
	writer, err := c.getWriter(pkg.Architecture)
	if err != nil {
		return err
	}
	return writer.Add(pkg)
}

// }}}

// PackageWriter {{{

// This writer represents a Package list - which is to say, a list of
// binary .deb files, for a particular Architecture, in a particular Component
// in a particular Suite, in a particular Archive.
//
// This is not an encapsulation to store the entire Index in memory, rather,
// it's a wrapper to help write Package entries into the Index.
type PackageWriter struct {
	archive *Archive

	handle  *blobstore.Writer
	closer  func() error
	encoder *control.Encoder

	hashers []*transput.Hasher
}

// given a Suite, create a new Package Writer, configured with
// the appropriate Hashing, and targeting a new file blob in the
// underlying blobstore.
func newPackageWriter(suite *Suite) (*PackageWriter, error) {
	handle, err := suite.archive.store.Create()
	if err != nil {
		return nil, err
	}

	hashers := []*transput.Hasher{}
	writers := []io.Writer{handle}
	for _, algo := range suite.features.Hashes {
		hasher, err := transput.NewHasher(algo)
		if err != nil {
			handle.Close()
			return nil, err
		}
		hashers = append(hashers, hasher)
		writers = append(writers, hasher)
	}

	encoder, err := control.NewEncoder(io.MultiWriter(writers...))
	if err != nil {
		handle.Close()
		return nil, err
	}

	return &PackageWriter{
		archive: suite.archive,
		closer:  handle.Close,
		encoder: encoder,
		handle:  handle,
		hashers: hashers,
	}, nil
}

// Write a Package entry into the Packages index.
func (p PackageWriter) Add(pkg Package) error {
	return p.encoder.Encode(pkg)
}

// }}}

// vim: foldmethod=marker

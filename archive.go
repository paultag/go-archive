package archive

import (
	"fmt"
	"io"
	"path"
	"time"

	"crypto"
	"crypto/sha512"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/packet"

	"pault.ag/go/blobstore"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/transput"
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
	release := suite.newRelease()

	files := map[string]blobstore.Object{}

	for name, component := range suite.components {
		for arch, writer := range component.packageWriters {

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

	/* Now, let's do some magic */

	obj, sig, err := suite.archive.encodeSigned(release)
	if err != nil {
		return nil, err
	}

	filePath := path.Join("dists", suite.Name, "Release")
	files[filePath] = *obj
	files[fmt.Sprintf("%s.gpg", filePath)] = *sig

	return files, nil
}

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
	fmt.Printf("%x\n", hash.Sum(nil))

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

func newComponent(suite *Suite) (*Component, error) {
	return &Component{
		suite:          suite,
		packageWriters: map[dependency.Arch]*PackageWriter{},
	}, nil
}

type Component struct {
	suite          *Suite
	packageWriters map[dependency.Arch]*PackageWriter
	// sourceWriter *SourceWriter
}

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

func (c *Component) AddPackage(pkg Package) error {
	writer, err := c.getWriter(pkg.Architecture)
	if err != nil {
		return err
	}
	return writer.Add(pkg)
}

type packageWriters map[dependency.Arch]*PackageWriter

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

type PackageWriter struct {
	archive *Archive

	handle  *blobstore.Writer
	closer  func() error
	encoder *control.Encoder

	hashers []*transput.Hasher
}

func (p PackageWriter) Add(pkg Package) error {
	return p.encoder.Encode(pkg)
}

// vim: foldmethod=marker

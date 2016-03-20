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

// New {{{

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

// }}}

// Archive magic {{{

// Core Archive abstrcation. This contains helpers to write out package files,
// as well as handles creating underlying abstractions (such as Suites).
type Archive struct {
	store      blobstore.Store
	signingKey *openpgp.Entity
}

// Suite {{{

// Get a Suite for a given Archive. Information will be loaded from the
// InRelease file (if it exists) into the Suite object.
//
// The Suite object contains neat abstractions such as Components, and a
// number of helpers to collection information across all components, such
// as the `Arches()` helper.
func (a Archive) Suite(name string) (*Suite, error) {
	/* Get the Release / InRelease */
	inRelease := Release{}
	components := map[string]*Component{}

	fd, err := a.store.OpenPath(path.Join("dists", name, "InRelease"))
	if err == nil {
		defer fd.Close()
		if err := control.Unmarshal(&inRelease, fd); err != nil {
			return nil, err
		}

		for _, name := range inRelease.Components {
			components[name] = &Component{Packages: []Package{}}
		}
	}

	suite := Suite{
		Name:        name,
		Description: inRelease.Description,
		Origin:      inRelease.Origin,
		Label:       inRelease.Label,
		Version:     inRelease.Version,
		Suite:       inRelease.Suite,
		release:     inRelease,
		Components:  components,
	}

	suite.Pool = Pool{store: a.store, suite: &suite}
	suite.features.Hashes = []string{"sha256", "sha1"}

	return &suite, nil
}

// }}}

// Encoders {{{

// Encode (Hashed (from a Suite)) {{{

func (a Archive) encodeHashedBySuite(
	path string,
	suite Suite,
	data interface{},
) (*blobstore.Object, []control.FileHash, error) {

	hashers := []*transput.Hasher{}
	for _, algorithm := range suite.features.Hashes {
		hasher, err := transput.NewHasher(algorithm)
		if err != nil {
			return nil, nil, err
		}
		hashers = append(hashers, hasher)
	}

	return a.encodeHashed(path, hashers, data)
}

// }}}

// Encode (Hashed) {{{

func (a Archive) encodeHashed(
	path string,
	hashers []*transput.Hasher,
	data interface{},
) (*blobstore.Object, []control.FileHash, error) {

	writers := []io.Writer{}
	for _, hasher := range hashers {
		writers = append(writers, hasher)
	}

	obj, err := a.encode(data, io.MultiWriter(writers...))
	if err != nil {
		return nil, nil, err
	}

	fileHashs := []control.FileHash{}
	for _, hasher := range hashers {
		fileHashs = append(fileHashs, control.FileHashFromHasher(path, *hasher))
	}

	return obj, fileHashs, nil
}

// }}}

// Encode (Signed) {{{

func (a Archive) encodeSigned(
	data interface{},
) (*blobstore.Object, *blobstore.Object, error) {
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

// }}}

// Encode {{{

func (a Archive) encode(data interface{}, tee io.Writer) (*blobstore.Object, error) {
	writer, err := a.store.Create()
	if err != nil {
		return nil, err
	}
	defer writer.Close()

	var target io.Writer = writer
	if tee != nil {
		target = io.MultiWriter(writer, tee)
	}

	encoder, err := control.NewEncoder(target)
	if err != nil {
		return nil, err
	}

	if err := encoder.Encode(data); err != nil {
		return nil, err
	}

	obj, err := a.store.Commit(*writer)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// }}}

// }}}

// Engross {{{

// Given a fully formed (and modified!) Suite object, go ahead and Engross
// the object to the Archive blobstore.
//
// This call will return a map of paths to blobs, which can be passed to
// `Link` to swap all files in at once. Simply Engrossing the Suite won't
// actually publish it.
func (a Archive) Engross(suite Suite) (map[string]blobstore.Object, error) {
	files := map[string]blobstore.Object{}

	// duration, err := time.ParseDuration("24h")
	// if err != nil {
	// 	return nil, err
	// }
	when := time.Now()

	release := Release{
		Description:   suite.Description,
		Origin:        suite.Origin,
		Label:         suite.Label,
		Version:       suite.Version,
		Suite:         suite.Name,
		Codename:      "",
		Components:    suite.ComponenetNames(),
		Architectures: suite.Arches(),
		Date:          when.Format(time.RFC1123Z),
		// ValidUntil:    when.Add(duration).Format(time.RFC1123Z),
		SHA256: []control.SHA256FileHash{},
		SHA1:   []control.SHA1FileHash{},
		SHA512: []control.SHA512FileHash{},
		MD5Sum: []control.MD5FileHash{},
	}

	for name, component := range suite.Components {
		for arch, pkgs := range component.ByArch() {
			suitePath := path.Join(name, fmt.Sprintf("binary-%s", arch),
				"Packages")
			filePath := path.Join("dists", suite.Name, suitePath)

			obj, hashes, err := a.encodeHashedBySuite(suitePath, suite, pkgs)
			if err != nil {
				return nil, err
			}

			for _, hash := range hashes {
				if err := release.AddHash(hash); err != nil {
					return nil, err
				}
			}

			files[filePath] = *obj
		}
	}

	filePath := path.Join("dists", suite.Name, "Release")
	obj, sig, err := a.encodeSigned(release)
	if err != nil {
		return nil, err
	}
	files[filePath] = *obj
	files[fmt.Sprintf("%s.gpg", filePath)] = *sig

	return files, nil
}

// }}}

// Link {{{

// Given a mapping of paths to Objects, link all of those objects
// into the Archive. Objects, after Engrossed, are stored in the
// blobstore, but won't be actually published until they're linked
// into place.
func (a Archive) Link(blobs map[string]blobstore.Object) error {
	for p, obj := range blobs {
		if err := a.store.Link(obj, p); err != nil {
			return err
		}
	}
	return nil
}

// }}}

// Decruft {{{

// Use the default backend to remove any unlinked files from the Blob store.
func (a Archive) Decruft() error {
	return a.store.GC(blobstore.DumbGarbageCollector{})
}

// }}}

// }}}

// Suite magic {{{

type Suite struct {
	Name        string
	Description string
	Origin      string
	Label       string
	Version     string
	Suite       string

	release    Release
	Components map[string]*Component
	Pool       Pool

	features struct {
		Hashes []string
	}
}

// Arches {{{

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

// }}}

// ComponenetNames {{{

// Return a list of unique component names.
func (s Suite) ComponenetNames() []string {
	ret := []string{}
	for name, _ := range s.Components {
		ret = append(ret, name)
	}
	return ret
}

// }}}

// Add {{{

// Add a package `pkg` to the component `name`.
func (s Suite) Add(name string, pkg Package) {
	if _, ok := s.Components[name]; !ok {
		s.Components[name] = &Component{Packages: []Package{}}
	}
	s.Components[name].Add(pkg)
}

// }}}

// }}}

// Component magic {{{

// Component is a section of the Archive, which is a set of Binary packages
// that are provided to the end user. Debian has three major ones, `main`,
// `contrib` and `non-free`.
type Component struct {
	Packages []Package
}

// ByArch {{{

// For a Component, get a list of Packages to provide, but split them
// into a map keyed by the Package's Arch.
func (c *Component) ByArch() map[dependency.Arch][]Package {
	ret := map[dependency.Arch][]Package{}

	for _, pkg := range c.Packages {
		packages := ret[pkg.Architecture]
		ret[pkg.Architecture] = append(packages, pkg)
	}

	return ret
}

// }}}

// Arches {{{

// For a Component, get the unique architectures contained in the Binary
// packages.
func (c *Component) Arches() []dependency.Arch {
	ret := []dependency.Arch{}
	for _, pkg := range c.Packages {
		ret = append(ret, pkg.Architecture)
	}
	return ret
}

// }}}

// Add {{{

// Add a Package to the Component.
func (c *Component) Add(p Package) {
	c.Packages = append(c.Packages, p)
}

// }}}

// }}}

// vim: foldmethod=marker

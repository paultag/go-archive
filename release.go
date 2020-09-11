/* {{{ Copyright (c) Paul R. Tagliamonte <paultag@debian.org>, 2015
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE. }}} */

package archive

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/openpgp"
	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
)

// Release {{{

// The file "dists/$DIST/InRelease" shall contain meta-information about the
// distribution and checksums for the indices, possibly signed with a GPG
// clearsign signature (for example created by "gpg -a -s --clearsign"). For
// older clients there can also be a "dists/$DIST/Release" file without any
// signature and the file "dists/$DIST/Release.gpg" with a detached GPG
// signature of the "Release" file, compatible with the format used by the GPG
// options "-a -b -s".
type Release struct {
	control.Paragraph

	Description string

	// Optional field indicating the origin of the repository, a single line
	// of free form text.
	Origin string

	// Optional field including some kind of label, a single line of free form
	// text.
	//
	// Typically used extensively in repositories split over multiple media
	// such as repositories stored on CDs.
	Label string

	// The Version field, if specified, shall be the version of the release.
	// This is usually a sequence of integers separated by the character
	// "." (full stop).
	//
	// Example:
	//
	//   Version: 6.0
	Version string

	// The Suite field may describe the suite. A suite is a single word. In
	// Debian, this shall be one of oldstable, stable, testing, unstable,
	// or experimental; with optional suffixes such as -updates.
	//
	// Example:
	// //   Suite: stable
	Suite string

	// The Codename field shall describe the codename of the release. A
	// codename is a single word. Debian releases are codenamed after Toy
	// Story Characters, and the unstable suite has the codename sid, the
	// experimental suite has the codename experimental.
	//
	// Example:
	//
	//   Codename: squeeze
	Codename string

	// A whitespace separated list of areas.
	//
	// Example:
	//
	//   Components: main contrib non-free
	//
	// May also include be prefixed by parts of the path following the
	// directory beneath dists, if the Release file is not in a directory
	// directly beneath dists/. As an example, security updates are specified
	// in APT as:
	//
	// deb http://security.debian.org/debian-security stable-security main)
	//
	// The Release file would be located at
	// http://security.debian.org/dists/stable-security/Release and look like:
	//
	//   Suite: stable-security
	//   Components: main contrib non-free
	Components []string `delim:" "`

	// Whitespace separated unique single words identifying Debian machine
	// architectures as described in Architecture specification strings,
	// Section 11.1. Clients should ignore Architectures they do not know
	// about.
	Architectures []dependency.Arch

	// The Date field shall specify the time at which the Release file was
	// created. Clients updating a local on-disk cache should ignore a Release
	// file with an earlier date than the date in the already stored Release
	// file.
	//
	// The Valid-Until field may specify at which time the Release file should
	// be considered expired by the client. Client behaviour on expired Release
	// files is unspecified.
	//
	// The format of the dates is the same as for the Date field in .changes
	// files; and as used in debian/changelog files, and documented in Policy
	// 4.4 ( Debian changelog: debian/changelog)
	Date       string
	ValidUntil string `control:"Valid-Until"`

	// note the upper-case S in MD5Sum (unlike in Packages and Sources files)
	//
	// These fields are used for two purposes:
	//
	// describe what package index files are present when release signature is
	// available it certifies that listed index files and files referenced by
	// those index files are genuine Those fields shall be multi-line fields
	// containing multiple lines of whitespace separated data. Each line shall
	// contain
	//
	// The checksum of the file in the format corresponding to the field The
	// size of the file (integer >= 0) The filename relative to the directory
	// of the Release file Each datum must be separated by one or more
	// whitespace characters.
	//
	// Server requirements:
	//
	// The checksum and sizes shall match the actual existing files. If indexes
	// are compressed, checksum data must be provided for uncompressed files as
	// well, even if not present on the server.  Client behaviour:
	//
	// Any file should be checked at least once, either in compressed or
	// uncompressed form, depending on which data is available. If a file has
	// no associated data, the client shall inform the user about this under
	// possibly dangerous situations (such as installing a package from that
	// repository). If a file does not match the data specified in the release
	// file, the client shall not use any information from that file, inform
	// the user, and might use old information (such as the previous locally
	// kept information) instead.
	MD5Sum []control.MD5FileHash    `delim:"\n" strip:" \t\n\r" multiline:"true"`
	SHA1   []control.SHA1FileHash   `delim:"\n" strip:" \t\n\r" multiline:"true"`
	SHA256 []control.SHA256FileHash `delim:"\n" strip:" \t\n\r" multiline:"true"`
	SHA512 []control.SHA512FileHash `delim:"\n" strip:" \t\n\r" multiline:"true"`

	// The NotAutomatic and ButAutomaticUpgrades fields are optional boolean
	// fields instructing the package manager. They may contain the values
	// "yes" and "no". If one the fields is not specified, this has the same
	// meaning as a value of "no".
	//
	// If a value of "yes" is specified for the NotAutomatic field, a package
	// manager should not install packages (or upgrade to newer versions) from
	// this repository without explicit user consent (APT assigns priority 1 to
	// this) If the field ButAutomaticUpgrades is specified as well and has the
	// value "yes", the package manager should automatically install package
	// upgrades from this repository, if the installed version of the package
	// is higher than the version of the package in other sources (APT assigns
	// priority 100).
	//
	// Specifying "yes" for ButAutomaticUpgrades without specifying "yes" for
	// NotAutomatic is invalid.
	NotAutomatic         string
	ButAutomaticUpgrades string

	AcquireByHash bool `control:"Acquire-By-Hash"`
}

// Given a file declared in the Release file, get the FileHash entries
// for that file (SHA256, SHA512). These can be used to ensure the
// integrety of files in the archive.
func (r *Release) Indices() map[string]control.FileHashes {
	ret := map[string]control.FileHashes{}

	// https://wiki.debian.org/DebianRepository/Format#Size.2C_MD5sum.2C_SHA1.2C_SHA256.2C_SHA512:
	// Clients may not use the MD5Sum and SHA1 fields for security purposes, and
	// must require a SHA256 or a SHA512 field.

	for _, el := range r.SHA256 {
		ret[el.Filename] = append(ret[el.Filename], el.FileHash)
	}
	for _, el := range r.SHA512 {
		ret[el.Filename] = append(ret[el.Filename], el.FileHash)
	}
	return ret
}

func (r *Release) AddHash(h control.FileHash) error {
	switch h.Algorithm {
	case "sha256":
		r.SHA256 = append(r.SHA256, control.SHA256FileHash{h})
	case "sha1":
		r.SHA1 = append(r.SHA1, control.SHA1FileHash{h})
	case "sha512":
		r.SHA512 = append(r.SHA512, control.SHA512FileHash{h})
	case "md5":
		r.MD5Sum = append(r.MD5Sum, control.MD5FileHash{h})
	default:
		return fmt.Errorf("No known hash: '%s'", h.Algorithm)
	}
	return nil
}

// }}}

// LoadInRelease {{{

// Given an InRelease io.Reader, and the OpenPGP keyring
// to validate against, return the parsed InRelease file.
func LoadInRelease(in io.Reader, keyring *openpgp.EntityList) (*Release, error) {
	ret := Release{}
	decoder, err := control.NewDecoder(in, keyring)
	if err != nil {
		return nil, err
	}
	return &ret, decoder.Decode(&ret)
}

// }}}

// LoadInReleaseFile {{{

// Given a path to the InRelease file on the filesystem, and the OpenPGP keyring
// to validate against, return the parsed InRelease file.
func LoadInReleaseFile(path string, keyring *openpgp.EntityList) (*Release, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return LoadInRelease(fd, keyring)
}

// }}}

// vim: foldmethod=marker

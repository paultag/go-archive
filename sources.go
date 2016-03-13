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
	"io"
	"os"

	"pault.ag/go/debian/control"
	// "pault.ag/go/debian/deb"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/version"
)

// Source {{{

// The files dists/$DIST/$COMP/source/Sources are called Sources indices. They
// consist of multiple paragraphs, where each paragraph has the format defined
// in Policy 5.5 (5.4 Debian source control files -- .dsc), with the following
// changes and additional fields. The changes are:
//
//  - The "Source" field is renamed to "Package"
//  - A new mandatory field "Directory"
//  - A new optional field "Priority"
//  - A new optional field "Section"
//  - (Note that any fields present in .dsc files can end here as well, even if
//  - they are not documented by Debian policy, or not yet documented yet).
//
// Each paragraph shall begin with a "Package" field. Clients may also accept
// files where this is not the case.
type Source struct {
	control.Paragraph

	Package string

	Directory string `required:"true"`
	Priority  string
	Section   string

	Format           string
	Binaries         []string          `control:"Binary" delim:","`
	Architectures    []dependency.Arch `control:"Architecture"`
	Version          version.Version
	Origin           string
	Maintainer       string
	Uploaders        []string
	Homepage         string
	StandardsVersion string `control:"Standards-Version"`

	ChecksumsSha1   []control.SHA1FileHash   `control:"Checksums-Sha1" delim:"\n" strip:"\n\r\t "`
	ChecksumsSha256 []control.SHA256FileHash `control:"Checksums-Sha256" delim:"\n" strip:"\n\r\t "`
	Files           []control.MD5FileHash    `control:"Files" delim:"\n" strip:"\n\r\t "`
}

// Source Helpers {{{

func (s Source) BuildDepends() (*dependency.Dependency, error) {
	return dependency.Parse(s.Paragraph.Values["Build-Depends"])
}

// }}}

// }}}

// Sources {{{

type Sources struct {
	decoder *control.Decoder
}

// Next {{{

// Get the next Source entry in the Sources list. This will return an
// io.EOF at the last entry.
func (p *Sources) Next() (*Source, error) {
	next := Source{}
	return &next, p.decoder.Decode(&next)
}

// }}}

// LoadSourcesFile {{{

// Given a path, create a Sources iterator. Note that the Sources
// file is not OpenPGP signed, so one will need to verify the integrety
// of this file from the InRelease file before trusting any output.
func LoadSourcesFile(path string) (*Sources, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return LoadSources(fd)
}

// }}}

// LoadSources {{{

// Given an io.Reader, create a Sources iterator. Note that the Sources
// file is not OpenPGP signed, so one will need to verify the integrety
// of this file from the InRelease file before trusting any output.
func LoadSources(in io.Reader) (*Sources, error) {
	decoder, err := control.NewDecoder(in, nil)
	if err != nil {
		return nil, err
	}
	return &Sources{decoder: decoder}, nil
}

// }}}

// }}}

// vim: foldmethod=marker

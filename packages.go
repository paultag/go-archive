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
	"os"
	"strconv"

	"pault.ag/go/debian/control"
	"pault.ag/go/debian/deb"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/version"
)

// Package {{{

type Package struct {
	control.Paragraph

	Package       string `required:"true"`
	Source        string
	Version       version.Version `required:"true"`
	Section       string
	Priority      string
	Architecture  dependency.Arch `required:"true"`
	Essential     string
	InstalledSize int    `control:"Installed-Size"`
	Maintainer    string `required:"true"`
	Description   string `required:"true"`
	Homepage      string

	Filename       string `required:"true"`
	Size           int    `required:"true"`
	MD5sum         string
	SHA1           string
	SHA256         string
	DescriptionMD5 string `control:"Description-md5"`
}

// PackageFromDeb {{{

func PackageFromDeb(debFile deb.Deb) (*Package, error) {
	pkg := Package{}

	paragraph := debFile.Control.Paragraph
	paragraph.Set("Filename", debFile.Path)
	/* Now, let's do some magic */

	fd, err := os.Open(debFile.Path)
	if err != nil {
		return nil, err
	}
	stat, err := fd.Stat()
	if err != nil {
		return nil, err
	}
	paragraph.Set("Size", strconv.Itoa(int(stat.Size())))

	return &pkg, control.UnpackFromParagraph(debFile.Control.Paragraph, &pkg)
}

// }}}

// }}}

// Packages {{{

type Packages struct {
	decoder *control.Decoder
}

// Next {{{

func (p *Packages) Next() (*Package, error) {
	next := Package{}
	return &next, p.decoder.Decode(&next)
}

// }}}

// LoadPackages {{{

func LoadPackages(path string) (*Packages, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	/* Packages files aren't signed */
	decoder, err := control.NewDecoder(fd, nil)
	if err != nil {
		return nil, err
	}
	return &Packages{decoder: decoder}, nil
}

// }}}

// }}}

// vim: foldmethod=marker

package archive

import (
	"os"

	"pault.ag/go/debian/control"
	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/version"
)

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

type Packages struct {
	decoder *control.Decoder
}

func (p *Packages) Next() (*Package, error) {
	next := Package{}
	if err := p.decoder.Decode(&next); err != nil {
		return nil, err
	}
	return &next, nil
}

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

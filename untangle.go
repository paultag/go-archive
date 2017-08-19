package archive

import (
	"fmt"
	"io"
	"sort"

	"pault.ag/go/debian/dependency"
	"pault.ag/go/debian/version"
)

func SortPackages(packages []Package) []Package {
	sort.Slice(packages, func(i, j int) bool {
		return version.Compare(packages[i].Version, packages[j].Version) > 0
	})
	return packages
}

func SortSources(sources []Source) []Source {
	sort.Slice(sources, func(i, j int) bool {
		return version.Compare(sources[i].Version, sources[j].Version) > 0
	})
	return sources
}

type PackageMap map[string][]Package

func LoadPackageMap(binaries Packages) (*PackageMap, error) {
	ret := PackageMap{}

	for {
		binary, err := binaries.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		ret[binary.Package] = SortPackages(append(ret[binary.Package], *binary))
	}
	return &ret, nil
}

type SourceMap map[string][]Source

func LoadSourceMap(sources Sources) (*SourceMap, error) {
	ret := SourceMap{}

	for {
		source, err := sources.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		ret[source.Package] = SortSources(append(ret[source.Package], *source))
	}
	return &ret, nil
}

func (s SourceMap) Matches(possi dependency.Possibility) (int, error) {
	if possi.Arch != nil {
		return -1, fmt.Errorf("Arch is specified, but we're source! bad possi.")
	}
	candidates := s[possi.Name]
	if len(candidates) == 0 {
		return -1, fmt.Errorf("I have no idea what that source is!")
	}
	for i, candidate := range candidates {
		if possi.Version.SatisfiedBy(candidate.Version) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("No satisfactory dependency found")
}

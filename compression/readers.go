package compression

import (
	"io"
	"strings"

	"compress/bzip2"
	"compress/gzip"
	"xi2.org/x/xz"
)

type compressionReader func(io.Reader) (io.Reader, error)

func gzipNewReader(r io.Reader) (io.Reader, error) {
	return gzip.NewReader(r)
}

func xzNewReader(r io.Reader) (io.Reader, error) {
	return xz.NewReader(r, 0)
}

func bzipNewReader(r io.Reader) (io.Reader, error) {
	return bzip2.NewReader(r), nil
}

var knownReaders = map[string]compressionReader{
	".gz":  gzipNewReader,
	".bz2": bzipNewReader,
	".xz":  xzNewReader,
}

//
func Decompress(reader io.Reader, fileName string, tee io.Writer) (io.Reader, error) {
	if tee != nil {
		reader = io.TeeReader(reader, tee)
	}

	for suffix, decompressor := range knownReaders {
		if strings.HasSuffix(fileName, suffix) {
			newReader, err := decompressor(reader)
			if err != nil {
				return nil, err
			}
			return newReader, nil
		}
	}

	return reader, nil
}

// vim: foldmethod=marker

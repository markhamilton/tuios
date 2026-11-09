package session

import (
	"compress/flate"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Compression of a file stream's bytes.
//
// open-file-stream may send a file's bytes as one flate stream, at BestSpeed.
// flate is in the standard library and already in the binary, so it costs no
// size. A file is compressed only when it is worth it: not when its name says
// it is compressed already (the rsync skip list), and not when a sample of its
// first 64 KiB saves less than a tenth. The machine that reads the file
// decides, because it can read the sample without sending it.

// compressSampleSize is how much of a file the compression test reads.
const compressSampleSize = 64 << 10

// compressMinSize is the smallest file worth a flate stream: below it the
// stream's own bytes eat what it saves.
const compressMinSize = 512

// incompressibleExt is the rsync skip list: files whose format compresses
// them already.
var incompressibleExt = map[string]bool{}

func init() {
	for ext := range strings.FieldsSeq("3g2 3gp 7z aac ace apk avi bz2 deb dmg ear f4v flac flv gpg gz iso jar jpeg jpg lrz lz lz4 lzma lzo m1a m1v m2a m2ts m2v m4a m4b m4p m4r m4v mka mkv mov mp1 mp2 mp3 mp4 mpa mpeg mpg mpv mts odb odf odg odi odm odp ods odt oga ogg ogm ogv ogx opus otg oth otp ots ott oxt png qt rar rpm rz rzip spx squashfs sxc sxd sxg sxm sxw sz tbz tbz2 tgz tlz ts txz tzo vob war webm webp wma wmv xz z zip zst docx xlsx pptx pdf heic heif avif whl crx xpi") {
		incompressibleExt["."+ext] = true
	}
}

// compressibleName reports whether a file's name leaves it worth compressing.
func compressibleName(name string) bool {
	return !incompressibleExt[strings.ToLower(filepath.Ext(name))]
}

// compressibleSample reports whether flate at BestSpeed saves a tenth of the
// sample or more.
func compressibleSample(sample []byte) bool {
	if len(sample) < compressMinSize {
		return false
	}
	var c countWriter
	fw := getFlateWriter(&c)
	_, _ = fw.Write(sample)
	_ = fw.Close()
	putFlateWriter(fw)
	return c.n*10 < int64(len(sample))*9
}

// compressibleFile reports whether size bytes of f from off are worth a flate
// stream: the name, then a sample read with ReadAt, so f's offset stays.
func compressibleFile(f *os.File, name string, size, off int64) bool {
	if size < compressMinSize || !compressibleName(name) {
		return false
	}
	buf := make([]byte, min(size, compressSampleSize))
	n, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	return compressibleSample(buf[:n])
}

// countWriter counts what is written to it.
type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

var flateWriters = sync.Pool{New: func() any {
	fw, _ := flate.NewWriter(io.Discard, flate.BestSpeed)
	return fw
}}

// getFlateWriter is a BestSpeed flate writer to w, from a pool: a new one
// allocates its whole window, and a folder copy writes one per file.
func getFlateWriter(w io.Writer) *flate.Writer {
	fw := flateWriters.Get().(*flate.Writer)
	fw.Reset(w)
	return fw
}

func putFlateWriter(fw *flate.Writer) {
	fw.Reset(io.Discard)
	flateWriters.Put(fw)
}

var flateReaders sync.Pool

// getFlateReader is a flate reader of r, from a pool. r must be a
// bufio.Reader or another io.ByteReader, so the reader takes no byte past the
// end of the stream and the next record can be read after it.
func getFlateReader(r io.Reader) io.ReadCloser {
	if fr, ok := flateReaders.Get().(io.ReadCloser); ok {
		if err := fr.(flate.Resetter).Reset(r, nil); err == nil {
			return fr
		}
	}
	return flate.NewReader(r)
}

func putFlateReader(fr io.ReadCloser) {
	flateReaders.Put(fr)
}

// flateEnd reads a flate stream to its end after the bytes a copy expected,
// which must be all of it: a stream that holds more is not the file it says.
func flateEnd(fr io.Reader) error {
	var one [1]byte
	n, err := io.ReadFull(fr, one[:])
	switch {
	case n > 0:
		return errors.New("the compressed bytes hold more than the file's size")
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return nil
	}
	return err
}

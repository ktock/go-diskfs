package squashfs

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io/ioutil"

	"github.com/pierrec/lz4"
	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"
)

type compressor interface {
	compress([]byte) ([]byte, error)
	decompress([]byte) ([]byte, error)
	loadOptions([]byte) error
	optionsBytes() []byte
}

// lzma compression
type compressorLzma struct {
}

func (c *compressorLzma) compress(in []byte) ([]byte, error) {
	var b bytes.Buffer
	lz, err := lzma.NewWriter(&b)
	if err != nil {
		return nil, fmt.Errorf("Error creating lzma compressor: %v", err)
	}
	if _, err := lz.Write(in); err != nil {
		return nil, err
	}
	if err := lz.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (c *compressorLzma) decompress(in []byte) ([]byte, error) {
	b := bytes.NewReader(in)
	lz, err := lzma.NewReader(b)
	if err != nil {
		return nil, fmt.Errorf("Error creating lzma decompressor: %v", err)
	}
	p, err := ioutil.ReadAll(lz)
	if err != nil {
		return nil, fmt.Errorf("Error decompressing: %v", err)
	}
	return p, nil
}
func (c *compressorLzma) loadOptions(b []byte) error {
	// lzma has no supported optiosn
	return nil
}
func (c *compressorLzma) optionsBytes() []byte {
	return []byte{}
}

type gzipStrategy uint16

const (
	gzipDefault          gzipStrategy = 0x1
	gzipFiltered                      = 0x2
	gzipHuffman                       = 0x4
	gzipRunLengthEncoded              = 0x8
	gzipFixed                         = 0x10
)

// gzip compression
type compressorGzip struct {
	compressionLevel uint32
	windowSize       uint16
	strategies       map[gzipStrategy]bool
}

func (c *compressorGzip) compress(in []byte) ([]byte, error) {
	var b bytes.Buffer
	gz, err := gzip.NewWriterLevel(&b, int(c.compressionLevel))
	if err != nil {
		return nil, fmt.Errorf("Error creating gzip compressor: %v", err)
	}
	if _, err := gz.Write(in); err != nil {
		return nil, err
	}
	if err := gz.Flush(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (c *compressorGzip) decompress(in []byte) ([]byte, error) {
	b := bytes.NewReader(in)
	gz, err := gzip.NewReader(b)
	if err != nil {
		return nil, fmt.Errorf("Error creating gzip decompressor: %v", err)
	}
	p, err := ioutil.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("Error decompressing: %v", err)
	}
	return p, nil
}

func (c *compressorGzip) loadOptions(b []byte) error {
	expected := 8
	if len(b) != expected {
		return fmt.Errorf("Cannot parse gzip options, received %d bytes expected %d", len(b), expected)
	}
	c.compressionLevel = binary.LittleEndian.Uint32(b[0:4])
	c.windowSize = binary.LittleEndian.Uint16(b[4:6])
	strategies := map[gzipStrategy]bool{}
	flags := binary.LittleEndian.Uint16(b[6:8])
	for _, strategy := range []gzipStrategy{gzipDefault, gzipFiltered, gzipHuffman, gzipRunLengthEncoded, gzipFixed} {
		if flags&uint16(strategy) == uint16(strategy) {
			strategies[strategy] = true
		}
	}
	c.strategies = strategies
	return nil
}
func (c *compressorGzip) optionsBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], c.compressionLevel)
	binary.LittleEndian.PutUint16(b[4:6], c.windowSize)
	var flags uint16
	for _, strategy := range []gzipStrategy{gzipDefault, gzipFiltered, gzipHuffman, gzipRunLengthEncoded, gzipFixed} {
		if c.strategies[strategy] {
			flags |= uint16(strategy)
		}
	}
	binary.LittleEndian.PutUint16(b[6:8], flags)
	return b
}

// xz compression
type xzFilter uint32

const (
	xzFilterX86      xzFilter = 0x1
	xzFilterPowerPC           = 0x2
	xzFilterIA64              = 0x4
	xzFilterArm               = 0x8
	xzFilterArmThumb          = 0x10
	xzFilterSparc             = 0x20
)

type compressorXz struct {
	dictionarySize    uint32
	executableFilters map[xzFilter]bool
}

func (c *compressorXz) compress(in []byte) ([]byte, error) {
	var b bytes.Buffer
	config := xz.WriterConfig{
		DictCap: int(c.dictionarySize),
	}
	xz, err := config.NewWriter(&b)
	if err != nil {
		return nil, fmt.Errorf("Error creating xz compressor: %v", err)
	}
	if _, err := xz.Write(in); err != nil {
		return nil, err
	}
	if err := xz.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (c *compressorXz) decompress(in []byte) ([]byte, error) {
	b := bytes.NewReader(in)
	xz, err := xz.NewReader(b)
	if err != nil {
		return nil, fmt.Errorf("Error creating xz decompressor: %v", err)
	}
	p, err := ioutil.ReadAll(xz)
	if err != nil {
		return nil, fmt.Errorf("Error decompressing: %v", err)
	}
	return p, nil
}
func (c *compressorXz) loadOptions(b []byte) error {
	expected := 8
	if len(b) != expected {
		return fmt.Errorf("Cannot parse xz options, received %d bytes expected %d", len(b), expected)
	}
	c.dictionarySize = binary.LittleEndian.Uint32(b[0:4])
	filters := map[xzFilter]bool{}
	flags := binary.LittleEndian.Uint32(b[4:8])
	for _, filter := range []xzFilter{xzFilterX86, xzFilterPowerPC, xzFilterIA64, xzFilterArm, xzFilterArmThumb, xzFilterSparc} {
		if flags&uint32(filter) == uint32(filter) {
			filters[filter] = true
		}
	}
	c.executableFilters = filters
	return nil
}
func (c *compressorXz) optionsBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], c.dictionarySize)
	var flags uint32
	for _, filter := range []xzFilter{xzFilterX86, xzFilterPowerPC, xzFilterIA64, xzFilterArm, xzFilterArmThumb, xzFilterSparc} {
		if c.executableFilters[filter] {
			flags |= uint32(filter)
		}
	}
	binary.LittleEndian.PutUint32(b[4:8], flags)
	return b
}

// lz4 compression
type lz4Flag uint32

const (
	lz4HighCompression lz4Flag = 0x1
)
const (
	lz4version1 uint32 = 1
)

type compressorLz4 struct {
	version uint32
	flags   map[lz4Flag]bool
}

func (c *compressorLz4) compress(in []byte) ([]byte, error) {
	var b bytes.Buffer
	lz := lz4.NewWriter(&b)
	if _, err := lz.Write(in); err != nil {
		return nil, err
	}
	if err := lz.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (c *compressorLz4) decompress(in []byte) ([]byte, error) {
	b := bytes.NewReader(in)
	lz := lz4.NewReader(b)
	p, err := ioutil.ReadAll(lz)
	if err != nil {
		return nil, fmt.Errorf("Error decompressing: %v", err)
	}
	return p, nil
}
func (c *compressorLz4) loadOptions(b []byte) error {
	expected := 8
	if len(b) != expected {
		return fmt.Errorf("Cannot parse lz4 options, received %d bytes expected %d", len(b), expected)
	}
	version := binary.LittleEndian.Uint32(b[0:4])
	if version != lz4version1 {
		return fmt.Errorf("Compressed with lz4 version %d, only support %d", version, lz4version1)
	}
	c.version = version
	flagMap := map[lz4Flag]bool{}
	flags := binary.LittleEndian.Uint32(b[4:8])
	for _, f := range []lz4Flag{lz4HighCompression} {
		if flags&uint32(f) == uint32(f) {
			flagMap[f] = true
		}
	}
	c.flags = flagMap
	return nil
}
func (c *compressorLz4) optionsBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], c.version)
	var flags uint32
	for _, f := range []lz4Flag{lz4HighCompression} {
		if c.flags[f] {
			flags |= uint32(f)
		}
	}
	binary.LittleEndian.PutUint32(b[4:8], flags)
	return b
}

// zstd compression
type compressorZstd struct {
	level uint32
}

const (
	zstdMinLevel uint32 = 1
	zstdMaxLevel uint32 = 22
)

func (c *compressorZstd) loadOptions(b []byte) error {
	expected := 4
	if len(b) != expected {
		return fmt.Errorf("Cannot parse zstd options, received %d bytes expected %d", len(b), expected)
	}
	level := binary.LittleEndian.Uint32(b[0:4])
	if level < zstdMinLevel || level > zstdMaxLevel {
		return fmt.Errorf("zstd compression level requested %d, must be at least %d and not more thann %d", level, zstdMinLevel, zstdMaxLevel)
	}
	c.level = level
	return nil
}
func (c *compressorZstd) optionsBytes() []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b[0:4], c.level)
	return b
}

package squashfs

import (
	"encoding/binary"
	"io/ioutil"
	"os"
	"time"

	"github.com/diskfs/go-diskfs/util"
)

const (
	Squashfsfile             = "./testdata/file.sqs"
	SquashfsUncompressedfile = "./testdata/file_uncompressed.sqs"
)

// adder compression
type testCompressorAddBytes struct {
	b   []byte
	err error
}

func (c *testCompressorAddBytes) compress(b []byte) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	return b[:len(b)-len(c.b)], nil
}
func (c *testCompressorAddBytes) decompress(b []byte) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	return append(b, c.b...), nil
}
func (c *testCompressorAddBytes) loadOptions(b []byte) error {
	return nil
}
func (c *testCompressorAddBytes) optionsBytes() []byte {
	return []byte{}
}

var (
	testLargeDirEntryCount                 = 252
	testValidModTime                       = time.Unix(0x5c20d8d7, 0)
	testValidBlocksize              uint32 = 0x20000
	testFragmentStart               uint64 = 0x5082ac // this is not the start of the fragment ID table, but of the fragment table
	testValidSuperblockUncompressed        = &superblock{
		inodes:        0x1ff,
		modTime:       testValidModTime,
		blocksize:     testValidBlocksize,
		fragmentCount: 1,
		compression:   compressionGzip,
		idCount:       3,
		versionMajor:  4,
		versionMinor:  0,
		rootInode: &inodeRef{
			block:  0x4004,
			offset: 0x0158,
		},
		size:                0x50932b,
		idTableStart:        0x5092d6,
		xattrTableStart:     0x509313,
		inodeTableStart:     0x50196f,
		directoryTableStart: 0x505aed,
		fragmentTableStart:  0x5082be,
		exportTableStart:    0x5092c0,
		superblockFlags: superblockFlags{
			dedup:                 true,
			exportable:            true,
			uncompressedData:      true,
			uncompressedInodes:    true,
			uncompressedFragments: true,
		},
	}
	testMetaOffset        = testValidSuperblockUncompressed.inodeTableStart
	testDirectoryOffset   = testValidSuperblockUncompressed.directoryTableStart - testMetaOffset
	testFragmentOffset    = testFragmentStart - testMetaOffset
	testDirectoryDataSize = testFragmentOffset - testDirectoryOffset
	testIDTableStart      = 0x5092c8 + 2 - testMetaOffset
	testIDTableEnd        = 0x5092d6 - testMetaOffset
	testXattrIDStart      = 0x509301 + 2 - testMetaOffset
	testXattrIDEnd        = testValidSuperblockUncompressed.xattrTableStart - testMetaOffset
	testXattrMetaStart    = 0x5092de + 2 - testMetaOffset
	testXattrMetaEnd      = testXattrIDStart
	testFragEntries       = []*fragmentEntry{
		{size: 6415, compressed: false, start: 5242976},
	}
	testInodeSize = 8995
	testInodeHash = map[uint32]uint16{
		0x0: 0x0, 0x2002: 0x2000, 0x4004: 0x4000,
	}
	testInodeDataLength = 16760
	testDirectoryHash   = map[uint32]uint16{
		0x0: 0x0, 0x2002: 0x2000,
	}
	testDirectoryDataLength = 10171
)

func testGetFilesystem(f util.File) (*FileSystem, []byte, error) {
	file := f
	var (
		err error
		b   []byte
	)
	if file == nil {
		file, err = os.Open(SquashfsUncompressedfile)
		if err != nil {
			return nil, nil, err
		}
		b, err = ioutil.ReadFile(SquashfsUncompressedfile)
		if err != nil {
			return nil, nil, err
		}
	}
	blocksize := int64(testValidBlocksize)
	fs := &FileSystem{
		/*
			TODO: Still need to add these in
			uidsGids       []byte
			inodes         *hashedData
			directories    *hashedData
		*/
		fragments: []*fragmentEntry{
			&fragmentEntry{start: 200000, size: 12},
		},
		workspace:  "",
		compressor: compressorGzip{},
		size:       5251072,
		start:      0,
		file:       file,
		blocksize:  blocksize,
		xattrs:     nil,
		rootDir: &inodeImpl{
			header: &inodeHeader{
				inodeType: inodeBasicDirectory,
				uidIdx:    0,
				gidIdx:    1,
				modTime:   testValidSuperblockUncompressed.modTime,
				index:     1,
				mode:      0755,
			},
			body: &basicDirectory{
				startBlock:       0x2002,
				links:            5,
				fileSize:         0xaa,
				offset:           0x0714,
				parentInodeIndex: 0x0200,
			},
		},
		superblock: testValidSuperblockUncompressed,
	}
	return fs, b, nil
}

func testGetMetaBytes() ([]byte, uint64, error) {
	b, err := ioutil.ReadFile(SquashfsUncompressedfile)
	if err != nil {
		return nil, 0, err
	}
	// find where the inode table starts - that is the beginning of metadata
	metaStart := binary.LittleEndian.Uint64(b[64:72])
	return b[metaStart:], metaStart, nil
}

func testGetFilesystemRoot() []*directoryEntry {
	/*
		isSubdirectory bool
		name           string
		size           int64
		modTime        time.Time
		mode           os.FileMode
		inode          *inode
	*/

	// data taken from reading the bytes of the file SquashfsUncompressedfile
	modTime := time.Unix(0x5c20d8d7, 0)
	return []*directoryEntry{
		{true, "foo", 9949, modTime, 0755, nil, FileStat{0, 0, map[string]string{}}},
		{true, "zero", 32, modTime, 0755, nil, FileStat{0, 0, map[string]string{}}},
		{true, "random", 32, modTime, 0755, nil, FileStat{0, 0, map[string]string{}}},
		{false, "emptylink", 0, modTime, 0777, nil, FileStat{0, 0, map[string]string{}}},
		{false, "goodlink", 0, modTime, 0777, nil, FileStat{0, 0, map[string]string{}}},
		{false, "hardlink", 7, modTime, 0644, nil, FileStat{1, 2, map[string]string{}}},
		{false, "README.md", 7, modTime, 0644, nil, FileStat{1, 2, map[string]string{}}},
		{false, "attrfile", 5, modTime, 0644, nil, FileStat{0, 0, map[string]string{"abc": "def", "myattr": "hello"}}},
	}
}

// GetTestFileSmall get a *squashfs.File to a usable and known test file
func GetTestFileSmall(f util.File, c compressor) (*File, error) {
	fs, _, err := testGetFilesystem(f)
	if err != nil {
		return nil, err
	}
	fs.compressor = c
	ef := &extendedFile{
		startBlock:         superblockSize,
		fileSize:           7,
		sparse:             0,
		links:              0,
		fragmentBlockIndex: 0,
		fragmentOffset:     0,
		xAttrIndex:         0,
		blockSizes:         []*blockData{},
	}
	// inode 0, offset 0, name "README.md", type basic file
	return &File{
		extendedFile: ef,
		isReadWrite:  false,
		isAppend:     false,
		offset:       0,
		filesystem:   fs,
	}, nil
}

// GetTestFileBig get a *squashfs.File to a usable and known test file
func GetTestFileBig(f util.File, c compressor) (*File, error) {
	fs, _, err := testGetFilesystem(f)
	if err != nil {
		return nil, err
	}
	fs.compressor = c
	fragSize := uint64(5)
	size := uint64(fs.blocksize) + fragSize
	ef := &extendedFile{
		startBlock:         superblockSize,
		fileSize:           size,
		sparse:             0,
		links:              0,
		fragmentBlockIndex: 0,
		fragmentOffset:     7,
		xAttrIndex:         0,
		blockSizes: []*blockData{
			{size: uint32(fs.blocksize), compressed: false},
		},
	}
	// inode 0, offset 0, name "README.md", type basic file
	return &File{
		extendedFile: ef,
		isReadWrite:  false,
		isAppend:     false,
		offset:       0,
		filesystem:   fs,
	}, nil
}

func testEqualUint32Slice(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func CompareEqualMapStringString(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		ov, ok := b[k]
		if !ok {
			return false
		}
		if ov != v {
			return false
		}
	}
	return true
}

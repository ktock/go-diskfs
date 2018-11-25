package squashfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/testhelper"
)

func TestWorkspace(t *testing.T) {
	tests := []struct {
		fs *FileSystem
		ws string
	}{
		{&FileSystem{workspace: ""}, ""},
		{&FileSystem{workspace: "abc"}, "abc"},
	}
	for _, tt := range tests {
		ws := tt.fs.Workspace()
		if ws != tt.ws {
			t.Errorf("Mismatched workspace, actual '%s', expected '%s'", ws, tt.ws)
		}
	}
}

func TestFSType(t *testing.T) {
	fs := &FileSystem{}
	fsType := fs.Type()
	if fsType != filesystem.TypeSquashfs {
		t.Errorf("Mismatched type, actual '%v', expected '%v'", fsType, filesystem.TypeSquashfs)
	}
}

func TestValidateBlocksize(t *testing.T) {
	tests := []struct {
		size int64
		err  error
	}{
		{2, fmt.Errorf("blocksize %d too small, must be at least %d", 2, minBlocksize)},
		{minBlocksize - 1, fmt.Errorf("blocksize %d too small, must be at least %d", minBlocksize-1, minBlocksize)},
		{minBlocksize, nil},
		{maxBlocksize + 1, fmt.Errorf("blocksize %d too large, must be no more than %d", maxBlocksize+1, maxBlocksize)},
		{maxBlocksize, nil},
	}
	for _, tt := range tests {
		err := validateBlocksize(tt.size)
		if (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())) {
			t.Errorf("Mismatched errors for %d, actual then expected", tt.size)
			t.Logf("%v", err)
			t.Logf("%v", tt.err)
		}
	}
	for i := 12; i <= 20; i++ {
		size := int64(math.Exp2(float64(i)))
		err := validateBlocksize(size)
		if err != nil {
			t.Errorf("Unexpected erorr for size %d: %v", size, err)
		}
	}
}

func TestReadTable(t *testing.T) {
	b := []byte{0x5, 0x80, 1, 2, 3, 4, 5, 0x7, 0x80, 5, 6, 7, 8, 9, 10, 11}
	tests := []struct {
		b   []byte
		c   compressor
		err error
		h   *hashedData
	}{
		// we will not check compression here, as we checked it all with readMetaBlock()
		{b, nil, nil, &hashedData{
			hash: map[uint32]uint16{
				0: 0, 7: 5,
			},
			data: append(append([]byte{}, b[2:7]...), b[9:]...),
		},
		},
	}

	for i, tt := range tests {
		h, err := readTable(tt.b, tt.c)
		switch {
		case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
			t.Errorf("%d: mismatched error, actual then expected", i)
			t.Logf("%v", err)
			t.Logf("%v", tt.err)
		case (h == nil && tt.h != nil) || (h != nil && tt.h == nil) || (h != nil && tt.h != nil && !h.equal(tt.h)):
			t.Errorf("%d: mismatched header, actual then expected", i)
			t.Logf("%v", *h)
			t.Logf("%v", *tt.h)
		}
	}
}

func TestParseFragmentTable(t *testing.T) {
	b, offset, err := testGetMetaBytes()
	if err != nil {
		t.Fatalf("Error getting metadata bytes: %v", err)
	}
	startDirectories := testValidSuperblockUncompressed.directoryTableStart - testMetaOffset
	startFragID := testValidSuperblockUncompressed.fragmentTableStart - testMetaOffset
	bDirFrag := b[startDirectories:startFragID]
	fragmentBlocks := uint64((testValidSuperblockUncompressed.fragmentCount-1)/fragmentEntriesPerBlock + 1)
	// dir table + fragment table
	bIndex := b[startFragID : startFragID+uint64(fragmentBlocks)*8] // fragment index table

	// entries in the fragment ID table are offset from beginning of disk, not from frag table
	//   so need offset of bDirFrag from beginning of disk to make use of it
	entries, size, err := parseFragmentTable(bDirFrag, bIndex, offset+startDirectories, nil)
	if err != nil {
		t.Fatalf("Error reading fragment table: %v", err)
	}
	expectedSize := testFragmentStart
	if size != expectedSize {
		t.Errorf("Fragment table was size %d instead of expected %d", size, expectedSize)
	}
	expectedEntries := 1
	if len(entries) != expectedEntries {
		t.Errorf("Mismatched entries, has %d instead of expected %d", len(entries), expectedEntries)
	}
}

func TestParseXAttrsTable(t *testing.T) {
	// parseXattrsTable(bUIDXattr, bIndex []byte, offset uint64, c compressor) (*xAttrTable, error) {
	b, offset, err := testGetMetaBytes()
	if err != nil {
		t.Fatalf("Error getting metadata bytes: %v", err)
	}
	startUID := testValidSuperblockUncompressed.idTableStart - testMetaOffset
	startXattrsID := testValidSuperblockUncompressed.xattrTableStart - testMetaOffset
	bUIDXattr := b[startUID:startXattrsID]
	xattrIDBytes := testValidSuperblockUncompressed.size - testValidSuperblockUncompressed.xattrTableStart
	// dir table + fragment table
	bIndex := b[startUID : startUID+xattrIDBytes] // xattr index table

	// entries in the xattr ID table are offset from beginning of disk, not from xattr table
	//   so need offset of bUIDXattr from beginning of disk to make use of it
	table, err := parseXattrsTable(bUIDXattr, bIndex, offset+startUID, nil)
	if err != nil {
		t.Fatalf("Error reading xattrs table: %v", err)
	}
	expectedEntries := 1
	if len(table.list) != expectedEntries {
		t.Errorf("Mismatched entries, has %d instead of expected %d", len(table.list), expectedEntries)
	}
}

func TestReadXAttrsTable(t *testing.T) {
	s := &superblock{
		xattrTableStart: 2000,
		idTableStart:    1000,
		superblockFlags: superblockFlags{
			uncompressedXattrs: true,
		},
	}
	table := []byte{
		0x30, 0x80, // 0x30 bytes of uncompressed data
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
	}

	idTable := make([]byte, 2*xAttrIDEntrySize+2) // 2 entries plus the metadata block header

	xAttrIDStart := int64(s.xattrTableStart) - int64(len(idTable)) //
	xAttrStart := xAttrIDStart - int64(len(table))                 //

	list := []*xAttrIndex{
		{uint64(xAttrStart), 0x02, 0x10},
		{uint64(xAttrStart) + 0x10, 0x01, 0x10},
	}

	idTable[0] = uint8(len(idTable) - 2)
	idTable[1] = 0x80
	binary.LittleEndian.PutUint64(idTable[2:10], list[0].pos)
	binary.LittleEndian.PutUint32(idTable[10:14], list[0].count)
	binary.LittleEndian.PutUint32(idTable[14:18], list[0].size)
	binary.LittleEndian.PutUint64(idTable[18:26], list[1].pos)
	binary.LittleEndian.PutUint32(idTable[26:30], list[1].count)
	binary.LittleEndian.PutUint32(idTable[30:34], list[1].size)

	indexHeader := make([]byte, 16)
	binary.LittleEndian.PutUint64(indexHeader[:8], uint64(xAttrStart))
	binary.LittleEndian.PutUint32(indexHeader[8:12], 2)
	indexBody := make([]byte, 8)
	binary.LittleEndian.PutUint64(indexBody, uint64(xAttrIDStart))

	file := &testhelper.FileImpl{
		Reader: func(b []byte, offset int64) (int, error) {
			var b2 []byte
			switch offset {
			case xAttrStart: // xAttr meta block header
				b2 = table
			case xAttrStart + 2: // xAttr meta block
				b2 = table[2:]
			case xAttrIDStart: // index block heaeer
				b2 = idTable
			case xAttrIDStart + 2: // index block
				b2 = idTable[2:]
			case int64(s.xattrTableStart): // xattr ID block
				b2 = indexHeader
			case int64(s.xattrTableStart + xAttrHeaderSize): // xattr ID block minus the header
				b2 = indexBody
			}
			copy(b, b2)
			count := len(b2)
			if len(b) < len(b2) {
				count = len(b)
			}
			return count, io.EOF
		},
	}
	expectedTable := &xAttrTable{
		data: table[2:],
		list: list,
	}
	xtable, err := readXattrsTable(s, file, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	switch {
	case xtable == nil:
		t.Errorf("Unexpected xtable nil")
	case bytes.Compare(xtable.data, expectedTable.data) != 0:
		t.Errorf("Mismatched xtable.data, actual then expected")
		t.Logf("% x", xtable.data)
		t.Logf("% x", expectedTable.data)
	case len(xtable.list) != len(expectedTable.list):
		t.Errorf("Mismatched list, actual then expected")
		t.Logf("%#v", xtable.list)
		t.Logf("%#v", expectedTable.list)
	}
}

func TestReadFragmentTable(t *testing.T) {
	fs, _, err := testGetFilesystem(nil)
	if err != nil {
		t.Fatalf("Unable to read test file: %v", err)
	}
	entries, fragOffset, err := readFragmentTable(fs.superblock, fs.file, fs.compressor)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if fragOffset != testFragmentStart {
		t.Errorf("Mismatched fragment offset from directories, actual %d, expected %d", fragOffset, testFragmentStart)
	}
	if len(entries) != len(testFragEntries) {
		t.Errorf("Mismatched entries, actual %d expected %d", len(entries), len(testFragEntries))
	} else {
		for i, e := range entries {
			if *e != *testFragEntries[i] {
				t.Errorf("Mismatched entry %d, actual then expected", i)
				t.Logf("%#v", *e)
				t.Logf("%#v", *testFragEntries[i])
			}
		}
	}
}

func TestReadInodeTable(t *testing.T) {
	fs, b, err := testGetFilesystem(nil)
	if err != nil {
		t.Fatalf("Unable to read test file: %v", err)
	}
	data, err := readInodeTable(fs.superblock, fs.file, fs.compressor)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if data == nil {
		t.Fatalf("Unexpected nil data")
	}
	if len(data.hash) != len(testInodeHash) {
		t.Errorf("Mismatched inode hash, actual then expected")
		t.Logf("%#v", data.hash)
		t.Logf("%#v", testInodeHash)
	} else {
		for k, v := range data.hash {
			ov, ok := testInodeHash[k]
			switch {
			case !ok:
				t.Errorf("unexpected key %d", k)
			case ov != v:
				t.Errorf("mismatched value for key %d, actual %d, expected %d", k, v, ov)
			}
		}
	}
	if len(data.data) != testInodeDataLength {
		t.Errorf("mismatched data, actual %d expected %d", len(data.data), testInodeDataLength)
	}
	// compare first hundred to avoid the issue that every block size we have headers
	firstHundred := data.data[:100]
	expectedFirstHundred := b[fs.superblock.inodeTableStart+2 : fs.superblock.inodeTableStart+2+100]
	if bytes.Compare(firstHundred, expectedFirstHundred) != 0 {
		t.Errorf("mismatched data, actual then expected")
		t.Logf("%#v", firstHundred)
		t.Logf("%#v", expectedFirstHundred)
	}
}

func TestReadDirectoryTable(t *testing.T) {
	fs, b, err := testGetFilesystem(nil)
	if err != nil {
		t.Fatalf("Unable to read test file: %v", err)
	}
	data, err := readDirectoryTable(fs.superblock, testFragmentStart, fs.file, fs.compressor)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if data == nil {
		t.Fatalf("Unexpected nil data")
	}
	if len(data.hash) != len(testDirectoryHash) {
		t.Errorf("Mismatched directory hash, actual then expected")
		t.Logf("%#v", data.hash)
		t.Logf("%#v", testDirectoryHash)
	} else {
		for k, v := range data.hash {
			ov, ok := testDirectoryHash[k]
			switch {
			case !ok:
				t.Errorf("unexpected key %d", k)
			case ov != v:
				t.Errorf("mismatched value for key %d, actual %d, expected %d", k, v, ov)
			}
		}
	}
	if len(data.data) != testDirectoryDataLength {
		t.Errorf("mismatched data, actual %d expected %d", len(data.data), testDirectoryDataLength)
	}
	// compare first hundred to avoid the issue that every block size we have headers
	firstHundred := data.data[:100]
	expectedFirstHundred := b[fs.superblock.directoryTableStart+2 : fs.superblock.directoryTableStart+2+100]
	if bytes.Compare(firstHundred, expectedFirstHundred) != 0 {
		t.Errorf("mismatched data, actual then expected")
		t.Logf("%#v", firstHundred)
		t.Logf("%#v", expectedFirstHundred)
	}
}

func TestReadDirectory(t *testing.T) {
	// we will keep it simple, two tests:
	// 1- make sure it passes everything correctly
	// 2- make sure it handles errors correctly
	directories := &hashedData{}
	inodes := &hashedData{}
	blocksize := 2048
	p := "/a/b/c"

	tests := []*inodeTestImpl{
		&inodeTestImpl{
			size:    1000,
			iType:   inodeBasicFile,
			entries: nil,
			err:     fmt.Errorf("an error"),
		},
		&inodeTestImpl{
			size:    1000,
			iType:   inodeBasicFile,
			entries: []*directoryEntry{},
			err:     nil,
		},
	}
	for i, in := range tests {
		fs := &FileSystem{
			directories: directories,
			inodes:      inodes,
			blocksize:   int64(blocksize),
			rootDir:     in,
		}
		entries, err := fs.readDirectory(p)
		// just check that it called getDirectoryEntries
		switch {
		case (err != nil && in.err == nil) || (err == nil && in.err != nil):
			t.Errorf("%d: Mismatched error, actual then expected", i)
			t.Logf("%v", err)
			t.Logf("%v", in.err)
		case len(entries) != len(in.entries):
			t.Errorf("%d: mismatched entries, actual then expected", i)
			t.Logf("%v", entries)
			t.Logf("%v", in.entries)
		case in.getArgs[0] != p:
			t.Errorf("%d: mismatched path, actual %s, expected %s", i, in.getArgs[0], p)
		case !fs.inodes.equal(in.getArgs[1].(*hashedData)):
			t.Errorf("%d: mismatched inodes, actual %v, expected %v", i, in.getArgs[1], fs.inodes)
		case !fs.directories.equal(in.getArgs[2].(*hashedData)):
			t.Errorf("%d: mismatched directories, actual %v, expected %v", i, in.getArgs[2], fs.inodes)
		case in.getArgs[3] != int(fs.blocksize):
			t.Errorf("%d: mismatched blocksize, actual %d, expected %d", i, in.getArgs[3], fs.blocksize)
		}
	}
}

func TestReadBlock(t *testing.T) {
	location := int64(10000)
	smallLocation := int64(2000)
	size := uint32(20)
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	file := &testhelper.FileImpl{
		Reader: func(b []byte, offset int64) (int, error) {
			switch {
			case offset == location:
				copy(b, data)
				count := len(data)
				if len(b) < len(data) {
					count = len(b)
				}
				return count, io.EOF
			case offset == smallLocation:
				copy(b, data[:10])
				return 10, nil
			default:
				return 0, fmt.Errorf("unknown location")
			}
		},
	}

	tests := []struct {
		location   int64
		compressed bool
		compressor compressor
		data       []byte
		err        error
	}{
		{location, false, nil, data, nil},
		{smallLocation, false, nil, nil, fmt.Errorf("read %d bytes instead of expected %d", 10, 20)},
		{location + 25, false, nil, nil, fmt.Errorf("unknown location")},
		{location, true, &testCompressorAddBytes{b: []byte{0x25}}, append(data, 0x25), nil},
		{location, true, &testCompressorAddBytes{err: fmt.Errorf("foo")}, nil, fmt.Errorf("foo")},
	}
	for i, tt := range tests {
		fs := &FileSystem{
			file:       file,
			compressor: tt.compressor,
		}
		b, err := fs.readBlock(tt.location, tt.compressed, size)
		switch {
		case (err != nil && tt.err == nil) || (err == nil && tt.err != nil):
			t.Errorf("%d: mismatched error, actual then expected", i)
			t.Logf("%v", err)
			t.Logf("%v", tt.err)
		case bytes.Compare(b, tt.data) != 0:
			t.Errorf("%d: Mismatched data, actual then expected", i)
			t.Logf("% x", b)
			t.Logf("% x", tt.data)
		}
	}
}

func TestReadFragment(t *testing.T) {
	//func (fs *FileSystem) readFragment(index, offset uint32, fragmentSize int64) ([]byte, error) {
	fragments := []*fragmentEntry{
		{start: 0, size: 20, compressed: false},
		{start: 20, size: 10, compressed: false},
		{start: 30, size: 10, compressed: true},
	}
	data := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
		20, 21, 22, 23, 24, 25, 26, 27, 28, 29,
		30, 31, 32, 33, 34, 35, 36, 37, 38, 39}
	file := &testhelper.FileImpl{
		Reader: func(b []byte, offset int64) (int, error) {
			for _, f := range fragments {
				if uint64(offset) == f.start {
					copy(b, data[f.start:f.start+uint64(f.size)])
					return int(f.size), nil
				}
			}
			return 0, fmt.Errorf("unknown location")
		},
	}
	tests := []struct {
		index      uint32
		offset     uint32
		size       int64
		compressor compressor
		data       []byte
		err        error
	}{
		{0, 10, 5, nil, data[10:15], nil},
		{1, 2, 5, nil, data[22:27], nil},
		{2, 2, 5, nil, nil, fmt.Errorf("Fragment compressed but do not have valid compressor")},
		{2, 2, 9, &testCompressorAddBytes{b: []byte{0x40}}, append(data[32:40], 0x40), nil},
		{2, 2, 5, &testCompressorAddBytes{err: fmt.Errorf("foo")}, nil, fmt.Errorf("decompress error: foo")},
		{3, 2, 5, nil, nil, fmt.Errorf("cannot find fragment block with index %d", 3)},
	}

	for i, tt := range tests {
		fs := &FileSystem{
			fragments:  fragments,
			file:       file,
			compressor: tt.compressor,
		}
		b, err := fs.readFragment(tt.index, tt.offset, tt.size)
		switch {
		case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
			t.Errorf("%d: mismatched error, actual then expected", i)
			t.Logf("%v", err)
			t.Logf("%v", tt.err)
		case bytes.Compare(b, tt.data) != 0:
			t.Errorf("%d: Mismatched data, actual then expected", i)
			t.Logf("% x", b)
			t.Logf("% x", tt.data)
		}
	}
}

func TestReadUidsGids(t *testing.T) {
	//func readUidsGids(s *superblock, file util.File, c compressor) ([]uint32, error) {
	expected := []uint32{
		0, 10, 100, 1000,
	}
	ids := []byte{
		0x10, 0x80, // 16 bytes of uncompressed data
		0, 0, 0, 0,
		10, 0, 0, 0,
		100, 0, 0, 0,
		0xe8, 0x03, 0, 0,
	}
	idStart := uint64(1000)
	indexStart := idStart + uint64(len(ids))
	index := make([]byte, 8)
	binary.LittleEndian.PutUint64(index, idStart)
	s := &superblock{
		idTableStart: indexStart,
		idCount:      uint16(len(ids)-2) / 4,
	}
	file := &testhelper.FileImpl{
		Reader: func(b []byte, offset int64) (int, error) {
			switch uint64(offset) {
			case idStart:
				copy(b, ids)
				return len(ids), nil
			case idStart + 2:
				copy(b, ids[2:])
				return len(ids) - 2, nil
			case indexStart:
				copy(b, index)
				return len(index), nil
			default:
				return 0, fmt.Errorf("No data at position %d", offset)
			}
		},
	}
	uidsgids, err := readUidsGids(s, file, nil)
	switch {
	case err != nil:
		t.Errorf("Unexpected error: %v", err)
	case !testEqualUint32Slice(uidsgids, expected):
		t.Errorf("Mismatched results, actual then expected")
		t.Logf("%#v", uidsgids)
		t.Logf("%#v", expected)
	}
}

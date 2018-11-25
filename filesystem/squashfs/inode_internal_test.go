package squashfs

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// inode implementation for testing
type inodeTestImpl struct {
	size    int64
	iType   inodeType
	entries []*directoryEntry
	err     error
	getArgs []interface{}
}

func (i *inodeTestImpl) toBytes() []byte {
	return nil
}
func (i *inodeTestImpl) equal(o inode) bool {
	return false
}
func (i *inodeTestImpl) Size() int64 {
	return i.size
}
func (i *inodeTestImpl) inodeType() inodeType {
	return i.iType
}
func (i *inodeTestImpl) getDirectoryEntries(p string, inodes, directories *hashedData, uidgids []uint32, xAttrTable *xAttrTable, blocksize int) ([]*directoryEntry, error) {
	i.getArgs = make([]interface{}, 0)
	i.getArgs = append(i.getArgs, p)
	i.getArgs = append(i.getArgs, inodes)
	i.getArgs = append(i.getArgs, directories)
	i.getArgs = append(i.getArgs, blocksize)
	return i.entries, i.err
}

// first header
func testGetFirstInodeHeader() *inodeHeader {
	return &inodeHeader{
		inodeType: inodeType(9),
		mode:      0x01a4,
		uidIdx:    0,
		gidIdx:    1,
		modTime:   time.Unix(0x5c20d8d7, 0),
		index:     1,
	}
}
func testGetFirstInodeBody() inodeBody {
	return extendedFile{
		startBlock:         0,
		fragmentBlockIndex: 0,
		fileSize:           7,
		fragmentOffset:     0,
		sparse:             0,
		links:              2,
		xAttrIndex:         0xffffffff,
		blockSizes:         nil,
	}
}

/*
	basic file: 0x61-0x80
	extended file: 0x02-0x39
	basic directory: 0x826a-0x8289
	basic symlink: 0x3a-61
*/

func TestInodeType(t *testing.T) {
	iType := inodeType(102)
	in := &inodeImpl{
		header: &inodeHeader{
			inodeType: iType,
		},
	}
	out := in.inodeType()
	if out != iType {
		t.Errorf("Mismatched type, actual %d, expected %d", out, iType)
	}
}
func TestInodeSize(t *testing.T) {
	size := uint64(107)
	body := &extendedFile{
		fileSize: size,
	}
	in := &inodeImpl{
		body: body,
	}
	out := in.Size()
	if uint64(out) != size {
		t.Errorf("Mismatched size, actual %d, expected %d", out, size)
	}
}

func TestInodeHeader(t *testing.T) {
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}
	goodHeader := b[2 : 2+inodeHeaderSize]
	tests := []struct {
		b      []byte
		header *inodeHeader
		err    error
	}{
		{goodHeader, testGetFirstInodeHeader(), nil},
		{goodHeader[:10], nil, fmt.Errorf("Received only %d bytes instead of minimum %d", 10, inodeHeaderSize)},
	}
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			header, err := parseInodeHeader(tt.b)
			switch {
			case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
				t.Errorf("%d: mismatched error, actual then expected", i)
				t.Logf("%v", err)
				t.Logf("%v", tt.err)
			case header != nil && tt.header != nil && *header != *tt.header:
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("%#v", header)
				t.Logf("%#v", tt.header)
			}
		}
	})
	t.Run("toBytes", func(t *testing.T) {
		for i, tt := range tests {
			if tt.header == nil {
				continue
			}
			b := tt.header.toBytes()
			if bytes.Compare(b, tt.b) != 0 {
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("% x", b)
				t.Logf("% x", tt.b)
			}
		}

	})
}

func TestBlockData(t *testing.T) {
	tests := []struct {
		b   *blockData
		num uint32
	}{
		{&blockData{size: 0x212056, compressed: true}, 0x212056},
		{&blockData{size: 0x212056, compressed: false}, 0x1212056},
	}
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			b := parseBlockData(tt.num)
			if *b != *tt.b {
				t.Errorf("%d: mismatched output, actual then expected", i)
				t.Logf("%#v", b)
				t.Logf("%#v", tt.b)
			}
		}
	})
	t.Run("toUint32", func(t *testing.T) {
		for i, tt := range tests {
			num := tt.b.toUint32()
			if num != tt.num {
				t.Errorf("%d: mismatched output, actual %x expected %x", i, num, tt.num)
			}
		}
	})
}

func TestBasicDirectory(t *testing.T) {
	dir := &basicDirectory{
		startBlock:       0x0,
		links:            0x0,
		fileSize:         0x07,
		offset:           0x0,
		parentInodeIndex: 0x0,
	}
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}
	inodeB := b[2+0x10 : 2+0x10+0x10]
	tests := []struct {
		b   []byte
		dir *basicDirectory
		err error
	}{
		{inodeB[:], dir, nil},
		{inodeB[:10], nil, fmt.Errorf("Received %d bytes, fewer than minimum %d", 10, 16)},
	}

	t.Run("toBytes", func(t *testing.T) {
		for i, tt := range tests {
			if tt.dir == nil {
				continue
			}
			b := tt.dir.toBytes()
			if bytes.Compare(b, tt.b) != 0 {
				t.Errorf("%d: mismatched output, actual then expected", i)
				t.Logf("% x", b)
				t.Logf("% x", tt.b)
			}
		}
	})
	t.Run("Size", func(t *testing.T) {
		size := dir.size()
		if size != int64(dir.fileSize) {
			t.Errorf("mismatched sizes, actual %d expected %d", size, dir.fileSize)
		}
	})
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			d, err := parseBasicDirectory(tt.b)
			switch {
			case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
				t.Errorf("%d: mismatched error, actual then expected", i)
				t.Logf("%v", err)
				t.Logf("%v", tt.err)
			case d != nil && tt.dir != nil && *d != *tt.dir:
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("%#v", *dir)
				t.Logf("%#v", *tt.dir)
			}
		}
	})
}

func TestExtendedDirectory(t *testing.T) {
	// do some day when we have good raw data

	//func (i extendedDirectory) toBytes() []byte {
	// func (i extendedDirectory) size() int64 {
	// func parseExtendedDirectory(b []byte) (*extendedDirectory, error) {
}

func TestBasicFile(t *testing.T) {
	f := &basicFile{
		startBlock:         0,
		fragmentBlockIndex: 0,
		fileSize:           0xb,
		fragmentOffset:     0xc,
		blockSizes:         []*blockData{},
	}
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}

	inodeB := b[0x99+0x10 : 0x99+0x10+0x10]
	tests := []struct {
		b    []byte
		file *basicFile
		err  error
	}{
		{inodeB[:], f, nil},
		{inodeB[:10], nil, fmt.Errorf("Received %d bytes, fewer than minimum %d", 10, 16)},
	}

	t.Run("toBytes", func(t *testing.T) {
		for i, tt := range tests {
			if tt.file == nil {
				continue
			}
			b := tt.file.toBytes()
			if bytes.Compare(b, tt.b) != 0 {
				t.Errorf("%d: mismatched output, actual then expected", i)
				t.Logf("% x", b)
				t.Logf("% x", tt.b)
			}
		}
	})
	t.Run("Size", func(t *testing.T) {
		size := f.size()
		if size != int64(f.fileSize) {
			t.Errorf("mismatched sizes, actual %d expected %d", size, f.fileSize)
		}
	})
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			fl, err := parseBasicFile(tt.b, int(testValidBlocksize))
			switch {
			case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
				t.Errorf("%d: mismatched error, actual then expected", i)
				t.Logf("%v", err)
				t.Logf("%v", tt.err)
			case (fl == nil && tt.file != nil) || (fl != nil && tt.file == nil) || (fl != nil && tt.file != nil && !fl.equal(*tt.file)):
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("%#v", *fl)
				t.Logf("%#v", *tt.file)
			}
		}
	})
	t.Run("toExtended", func(t *testing.T) {
		// func (i basicFile) toExtended() extendedFile {
		ext := f.toExtended()
		if ext.size() != f.size() {
			t.Errorf("Mismatched sizes actual %d expected %d", ext.size(), f.size())
		}
		if ext.startBlock != uint64(f.startBlock) {
			t.Errorf("Mismatched startBlock actual %d expected %d", ext.startBlock, f.startBlock)
		}
		if ext.fragmentOffset != f.fragmentOffset {
			t.Errorf("Mismatched fragmentOffset actual %d expected %d", ext.fragmentOffset, f.fragmentOffset)
		}
		if ext.fragmentBlockIndex != f.fragmentBlockIndex {
			t.Errorf("Mismatched fragmentBlockIndex actual %d expected %d", ext.fragmentBlockIndex, f.fragmentBlockIndex)
		}
		if len(ext.blockSizes) != len(f.blockSizes) {
			t.Errorf("Mismatched blockSizes actual then expected")
			t.Logf("%#v", ext.blockSizes)
			t.Logf("%#v", f.blockSizes)
		}
	})
}

func TestExtendedFile(t *testing.T) {
	fd := testGetFirstInodeBody().(extendedFile)
	f := &fd
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}
	inodeB := b[0x12:0x3a]
	tests := []struct {
		b    []byte
		file *extendedFile
		err  error
	}{
		{inodeB[:], f, nil},
		{inodeB[:10], nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", 10, 40)},
	}

	t.Run("toBytes", func(t *testing.T) {
		for i, tt := range tests {
			if tt.file == nil {
				continue
			}
			b := tt.file.toBytes()
			if bytes.Compare(b, tt.b) != 0 {
				t.Errorf("%d: mismatched output, actual then expected", i)
				t.Logf("% x", b)
				t.Logf("% x", tt.b)
			}
		}
	})
	t.Run("Size", func(t *testing.T) {
		size := f.size()
		if size != int64(f.fileSize) {
			t.Errorf("mismatched sizes, actual %d expected %d", size, f.fileSize)
		}
	})
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			fl, err := parseExtendedFile(tt.b, int(testValidBlocksize))
			switch {
			case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
				t.Errorf("%d: mismatched error, actual then expected", i)
				t.Logf("%v", err)
				t.Logf("%v", tt.err)
			case (fl == nil && tt.file != nil) || (fl != nil && tt.file == nil) || (fl != nil && tt.file != nil && !fl.equal(*tt.file)):
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("%#v", *fl)
				t.Logf("%#v", *tt.file)
			}
		}
	})

}

func TestBasicSymlink(t *testing.T) {
	// 	basic symlink: 0x4a-61

	s := &basicSymlink{
		links:  1,
		target: "/a/b/c/d/ef/g/h",
	}
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}

	symLoc := 0x82
	size := 0x17
	inodeB := b[symLoc : symLoc+size]
	tests := []struct {
		b   []byte
		sym *basicSymlink
		err error
	}{
		{inodeB[:], s, nil},
		{inodeB[:10], nil, fmt.Errorf("Received %d bytes which is less than %d header plus indicated target size of %d", 10, 8, 15)},
	}

	t.Run("toBytes", func(t *testing.T) {
		for i, tt := range tests {
			if tt.sym == nil {
				continue
			}
			b := tt.sym.toBytes()
			if bytes.Compare(b, tt.b) != 0 {
				t.Errorf("%d: mismatched output, actual then expected", i)
				t.Logf("% x", b)
				t.Logf("% x", tt.b)
			}
		}
	})
	t.Run("Size", func(t *testing.T) {
		size := s.size()
		if size != 0 {
			t.Errorf("mismatched sizes, actual %d expected %d", size, 0)
		}
	})
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			sym, err := parseBasicSymlink(tt.b)
			switch {
			case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
				t.Errorf("%d: mismatched error, actual then expected", i)
				t.Logf("%v", err)
				t.Logf("%v", tt.err)
			case (sym == nil && tt.sym != nil) || (sym != nil && tt.sym == nil) || (sym != nil && tt.sym != nil && *sym != *tt.sym):
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("%#v", *sym)
				t.Logf("%#v", *tt.sym)
			}
		}
	})

}

func TestExtendedSymlink(t *testing.T) {
	// when we have more data with which to work

	//func (i extendedSymlink) toBytes() []byte {
	// func (i extendedSymlink) size() int64 {
	// func parseExtendedSymlink(b []byte) (*extendedSymlink, error) {
}

func TestBasicDevice(t *testing.T) {
	// when we have more data withh which to work

	//func (i basicDevice) toBytes() []byte {
	// func (i basicDevice) size() int64 {
	// func parseBasicDevice(b []byte) (*basicDevice, error) {
}

func TestExtendedDevice(t *testing.T) {
	// when we have more data withh which to work

	//func (i extendedDevice) toBytes() []byte {
	// func (i extendedDevice) size() int64 {
	// func parseExtendedDevice(b []byte) (*extendedDevice, error) {

}

func TestBasicIPC(t *testing.T) {
	// when we have more data withh which to work

	//func (i basicIPC) toBytes() []byte {
	// func (i basicIPC) size() int64 {
	// func parseBasicIPC(b []byte) (*basicIPC, error) {
}

func TestExtendedIPC(t *testing.T) {
	// when we have more data withh which to work

	// func (i extendedIPC) toBytes() []byte {
	// func (i extendedIPC) size() int64 {
	// func parseExtendedIPC(b []byte) (*extendedIPC, error) {
}

func TestInode(t *testing.T) {
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}
	inodeB := b[0x02:0x3a]
	in := &inodeImpl{
		header: testGetFirstInodeHeader(),
		body:   testGetFirstInodeBody(),
	}
	tests := []struct {
		b   []byte
		i   *inodeImpl
		err error
	}{
		{inodeB[:], in, nil},
		{inodeB[:10], nil, fmt.Errorf("Received %d bytes, insufficient for minimum %d for header and inode", 10, 17)},
	}

	t.Run("toBytes", func(t *testing.T) {
		for i, tt := range tests {
			if tt.i == nil {
				continue
			}
			b := tt.i.toBytes()
			if bytes.Compare(b, tt.b) != 0 {
				t.Errorf("%d: mismatched output, actual then expected", i)
				t.Logf("% x", b)
				t.Logf("% x", tt.b)
			}
		}
	})
	t.Run("parse", func(t *testing.T) {
		for i, tt := range tests {
			out, err := parseInode(tt.b, int(testValidBlocksize))
			switch {
			case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
				t.Errorf("%d: mismatched error, actual then expected", i)
				t.Logf("%v", err)
				t.Logf("%v", tt.err)
			case (out == nil && tt.i != nil) || (out != nil && tt.i == nil) || (out != nil && tt.i != nil && !out.equal(tt.i)):
				t.Errorf("%d: mismatched results, actual then expected", i)
				t.Logf("%#v", *out)
				t.Logf("%#v", *tt.i)
			}
		}
	})
}

func TestHydrateDirectoryEntries(t *testing.T) {
	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}
	dirBytes := b[testDirectoryOffset+2:]
	dir, err := parseDirectory(dirBytes, 151)
	if err != nil {
		t.Fatal(err)
	}
	entries := dir.entries
	inodeBytes := b[:testDirectoryOffset]
	inodes, err := readTable(inodeBytes, nil)
	if err != nil {
		t.Fatalf("error reading inode table: %v", err)
	}
	uidsgids := parseIDTable(b[testIDTableStart:testIDTableEnd])
	xattrs := &xAttrTable{}
	dirEntries, err := hydrateDirectoryEntries(entries, inodes, uidsgids, xattrs, int(testValidBlocksize))
	if err != nil {
		t.Fatalf("error hydrating directory entries: %v", err)
	}
	// just check the count for now
	if len(dirEntries) != testLargeDirEntryCount {
		t.Fatalf("Mismatched entry count: actual %d expected %d", len(dirEntries), testLargeDirEntryCount)
	}
}

func TestInodeGetDirectoryEntries(t *testing.T) {
	fs, _, err := testGetFilesystem(nil)
	if err != nil {
		t.Fatal(err)
	}

	b, _, err := testGetMetaBytes()
	if err != nil {
		t.Fatal(err)
	}
	dirBytes := b[testDirectoryOffset:testFragmentOffset]
	directories, err := readTable(dirBytes, nil)
	if err != nil {
		t.Fatalf("error reading directory table: %v", err)
	}
	dir, err := parseDirectory(dirBytes[2:], 151)
	if err != nil {
		t.Fatal(err)
	}
	uidsgids := parseIDTable(b[testIDTableStart:testIDTableEnd])
	entries := dir.entries
	inodeBytes := b[:testDirectoryOffset]
	inodes, err := readTable(inodeBytes, nil)
	if err != nil {
		t.Fatalf("error reading inode table: %v", err)
	}
	xattrs, err := parseXattrsTable(b[testXattrMetaStart:testXattrMetaEnd], b[testXattrIDStart:testXattrIDEnd], testValidSuperblockUncompressed.xattrTableStart, nil)
	if err != nil {
		t.Fatalf("Unable to read xattrs table: %v", err)
	}

	t.Run("bigdir", func(t *testing.T) {
		dirEntries, err := hydrateDirectoryEntries(entries, inodes, uidsgids, xattrs, int(testValidBlocksize))
		if err != nil {
			t.Fatalf("error hydrating directory entries: %v", err)
		}
		// just check the count for now
		if len(dirEntries) != testLargeDirEntryCount {
			t.Fatalf("Mismatched entry count: actual %d expected %d", len(dirEntries), testLargeDirEntryCount)
		}
	})
	t.Run("root", func(t *testing.T) {
		de, err := fs.rootDir.getDirectoryEntries("/", directories, inodes, uidsgids, xattrs, int(fs.blocksize))
		if err != nil {
			t.Fatal(err)
		}
		entries := testGetFilesystemRoot()
		if len(entries) != len(de) {
			t.Fatalf("Mismatched entries, actual %d expected %d", len(de), len(entries))
			t.Logf("%#v", de)
			t.Logf("%#v", entries)
		}
		// I do not care about order, only that they match, so we will sort them by name
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].name < entries[j].name
		})
		sort.Slice(de, func(i, j int) bool {
			return de[i].name < de[j].name
		})

		for i, e := range de {
			// for now going to ignore inode and filestat in tests
			e1 := &directoryEntry{}
			*e1 = *e
			e1.inode = nil
			f1 := &directoryEntry{}
			*f1 = *entries[i]
			if !e1.equal(f1) {
				t.Errorf("%d: mismatched entry, actual then expected", i)
				t.Logf("%#v", *e)
				t.Logf("%#v", *entries[i])
			}
		}
	})
}

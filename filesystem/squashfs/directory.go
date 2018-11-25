package squashfs

import (
	"encoding/binary"
	"fmt"
)

const (
	maxDirEntries   = 256
	dirHeaderSize   = 12
	dirEntryMinSize = 8
	dirNameMaxSize  = 256
)

type directory struct {
	entries []*directoryEntryRaw
}

type directoryEntryRaw struct {
	offset         uint16
	inodeNumber    uint16
	inodeType      inodeType
	name           string
	isSubdirectory bool
	startBlock     uint32
}

func (d *directoryEntryRaw) toBytes() []byte {
	b := make([]byte, 8)
	nameBytes := []byte(d.name)
	binary.LittleEndian.PutUint16(b[0:2], d.offset)
	binary.LittleEndian.PutUint16(b[2:4], d.inodeNumber)
	binary.LittleEndian.PutUint16(b[4:6], uint16(d.inodeType))
	binary.LittleEndian.PutUint16(b[6:8], uint16(len(nameBytes)-1))
	b = append(b, nameBytes...)
	return b
}

type directoryHeader struct {
	count      uint32
	startBlock uint32
	inode      uint32
}

// parse raw bytes of a directory to get the contents
func parseDirectory(b []byte, size uint32) (*directory, error) {
	entries := make([]*directoryEntryRaw, 0)
	// if size == 3, it is an empty directory
	if size == 3 {
		return &directory{
			entries: entries,
		}, nil
	}
	for pos := 0; pos < int(size-3); {
		directoryHeader, err := parseDirectoryHeader(b[pos:])
		if err != nil {
			return nil, fmt.Errorf("Could not parse directory header: %v", err)
		}
		if directoryHeader.count+1 > maxDirEntries {
			return nil, fmt.Errorf("Corrupted directory, had %d entries instead of max %d", directoryHeader.count+1, maxDirEntries)
		}
		pos += dirHeaderSize
		for count := uint32(0); count < directoryHeader.count; count++ {
			entry, err := parseDirectoryEntry(b[pos:])
			if err != nil {
				return nil, fmt.Errorf("Unable to parse entry at position %d: %v", pos, err)
			}
			entry.startBlock = directoryHeader.startBlock
			entries = append(entries, entry)
			// increment the position
			pos += len(entry.toBytes())
		}
	}

	return &directory{
		entries: entries,
	}, nil
}

func (d *directory) toBytes(startBlock, in uint32) []byte {
	b := make([]byte, 0)
	// create the header
	header := &directoryHeader{
		count:      uint32(len(d.entries)),
		startBlock: startBlock,
		inode:      in,
	}
	b = append(b, header.toBytes()...)
	for _, e := range d.entries {
		b = append(b, e.toBytes()...)
	}
	return b
}

func (d *directory) equal(b *directory) bool {
	if d == nil && b == nil {
		return true
	}
	if (d == nil && b != nil) || (d != nil && b == nil) {
		return false
	}
	// entries
	if len(d.entries) != len(b.entries) {
		return false
	}
	for i, e := range d.entries {
		if *e != *b.entries[i] {
			return false
		}
	}
	return true
}

// parse the header of a directory
func parseDirectoryHeader(b []byte) (*directoryHeader, error) {
	if len(b) < dirHeaderSize {
		return nil, fmt.Errorf("Header was %d bytes, less than minimum %d", len(b), dirHeaderSize)
	}
	return &directoryHeader{
		count:      binary.LittleEndian.Uint32(b[0:4]) + 1,
		startBlock: binary.LittleEndian.Uint32(b[4:8]),
		inode:      binary.LittleEndian.Uint32(b[8:12]),
	}, nil
}
func (d *directoryHeader) toBytes() []byte {
	b := make([]byte, dirHeaderSize)

	binary.LittleEndian.PutUint32(b[0:4], d.count-1)
	binary.LittleEndian.PutUint32(b[4:8], d.startBlock)
	binary.LittleEndian.PutUint32(b[8:12], d.inode)
	return b
}

// parse a raw directory entry
func parseDirectoryEntry(b []byte) (*directoryEntryRaw, error) {
	/*
		unsigned short		offset;
		short			inode_number;
		unsigned short		type;
		unsigned short		size;
		char			name[0];
	*/
	// ensure we have enough bytes to parse
	if len(b) < dirEntryMinSize {
		return nil, fmt.Errorf("Directory entry was %d bytes, less than minimum %d", len(b), dirEntryMinSize)
	}

	offset := binary.LittleEndian.Uint16(b[0:2])
	inode := binary.LittleEndian.Uint16(b[2:4])
	entryType := binary.LittleEndian.Uint16(b[4:6])
	nameSize := binary.LittleEndian.Uint16(b[6:8])
	realNameSize := nameSize + 1

	// make sure name is legitimate size
	if nameSize > dirNameMaxSize {
		return nil, fmt.Errorf("Name size was %d bytes, greater than maximum %d", nameSize, dirNameMaxSize)
	}
	if int(realNameSize+dirEntryMinSize) > len(b) {
		return nil, fmt.Errorf("Dir entry plus size of name is %d, larger than available bytes %d", nameSize+dirEntryMinSize, len(b))
	}

	// read in the name
	name := string(b[8 : 8+realNameSize])
	iType := inodeType(entryType)
	return &directoryEntryRaw{
		offset:         offset,
		inodeNumber:    inode,
		inodeType:      iType,
		isSubdirectory: iType == inodeBasicDirectory || iType == inodeExtendedDirectory,
		name:           name,
	}, nil

}

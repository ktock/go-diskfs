package squashfs

import (
	"encoding/binary"
	"fmt"
	"os"
	"path"
	"time"
)

type inodeType uint16

const (
	inodeBasicDirectory    inodeType = 1
	inodeBasicFile                   = 2
	inodeBasicSymlink                = 3
	inodeBasicBlock                  = 4
	inodeBasicChar                   = 5
	inodeBasicFifo                   = 6
	inodeBasicSocket                 = 7
	inodeExtendedDirectory           = 8
	inodeExtendedFile                = 9
	inodeExtendedSymlink             = 10
	inodeExtendedBlock               = 11
	inodeExtendedChar                = 12
	inodeExtendedFifo                = 13
	inodeExtendedSocket              = 14
)

const (
	inodeHeaderSize = 16
)

type inodeHeader struct {
	inodeType inodeType
	uidIdx    uint16
	gidIdx    uint16
	modTime   time.Time
	index     uint32
	mode      os.FileMode
	// permissions
}
type inodeBody interface {
	toBytes() []byte
	size() int64
	xattrIndex() (uint32, bool)
	equal(inodeBody) bool
}
type inode interface {
	toBytes() []byte
	equal(inode) bool
	Size() int64
	inodeType() inodeType
	getDirectoryEntries(string, *hashedData, *hashedData, []uint32, *xAttrTable, int) ([]*directoryEntry, error)
}
type inodeImpl struct {
	header *inodeHeader
	body   inodeBody
}

func (i *inodeImpl) equal(o inode) bool {
	other, ok := o.(*inodeImpl)
	if !ok {
		return false
	}
	if (i.header == nil && other.header != nil) || (i.header != nil && other.header == nil) || (i.header != nil && other.header != nil && *i.header != *other.header) {
		return false
	}
	if (i.body == nil && other.body != nil) || (i.body != nil && other.body == nil) || (i.body != nil && other.body != nil && !i.body.equal(other.body)) {
		return false
	}
	return true
}
func (i *inodeImpl) toBytes() []byte {
	h := i.header.toBytes()
	b := i.body.toBytes()
	return append(h, b...)
}
func (i *inodeImpl) inodeType() inodeType {
	return i.header.inodeType
}

// Size return the size of the item reflected by this inode, if it supports it
func (i *inodeImpl) Size() int64 {
	return i.body.size()
}
func (i *inodeImpl) getDirectoryEntries(p string, directories, inodes *hashedData, uidgids []uint32, xAttrTable *xAttrTable, blocksize int) ([]*directoryEntry, error) {
	var (
		block  uint32
		offset uint16
		size   uint32
	)

	// break path down into parts and levels
	parts, err := splitPath(p)
	if err != nil {
		return nil, fmt.Errorf("Could not parse path: %v", err)
	}

	iType := i.inodeType()
	size = uint32(i.body.size())
	switch iType {
	case inodeBasicDirectory:
		dir := i.body.(*basicDirectory)
		block = dir.startBlock
		offset = dir.offset
	case inodeExtendedDirectory:
		dir := i.body.(*extendedDirectory)
		block = dir.startBlock
		offset = dir.offset
	default:
		return nil, fmt.Errorf("inode is of type %d, neither basic nor extended directory", iType)
	}
	// read the directory data from the directory table
	dirData, err := directories.find(block, offset)
	if err != nil {
		return nil, fmt.Errorf("Unable to read directory from table: %v", err)
	}

	// parse the directory entries
	dir, err := parseDirectory(dirData[:size], size)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse directory entry at %s: %v", p, err)
	}
	entriesRaw := dir.entries
	var entries []*directoryEntry
	// if this is the directory we are looking for, return the entries
	if len(parts) == 0 {
		entries, err = hydrateDirectoryEntries(entriesRaw, inodes, uidgids, xAttrTable, blocksize)
		if err != nil {
			return nil, fmt.Errorf("Could not populate directory entries for %s with properties: %v", p, err)
		}
		return entries, nil
	}

	// it is not, so dig down one level
	// find the entry among the children that has the desired name
	for _, entry := range entriesRaw {
		// only care if not self or parent entry
		checkFilename := entry.name
		if checkFilename == parts[0] {
			// read the inode for this entry
			inodeData, err := inodes.find(uint32(entry.startBlock), entry.offset)
			if err != nil {
				return nil, fmt.Errorf("Error finding inode for %s: %v", p, err)
			}
			// parse the inode
			inode, err := parseInode(inodeData, blocksize)
			if err != nil {
				return nil, fmt.Errorf("Unable to parse inode for %s: %v", p, err)
			}

			childPath := ""
			if len(parts) > 1 {
				childPath = path.Join(parts[1:]...)
			}
			entries, err = inode.getDirectoryEntries(childPath, directories, inodes, uidgids, xAttrTable, blocksize)
			if err != nil {
				return nil, fmt.Errorf("Could not get entries: %v", err)
			}
			return entries, nil
		}
	}
	// if we made it here, we were not looking for this directory, but did not find it among our children
	return nil, fmt.Errorf("Could not find path %s", p)
}

func (i *inodeHeader) toBytes() []byte {
	b := make([]byte, inodeHeaderSize)
	binary.LittleEndian.PutUint16(b[0:2], uint16(i.inodeType))
	binary.LittleEndian.PutUint16(b[2:4], uint16(i.mode))
	binary.LittleEndian.PutUint16(b[4:6], i.uidIdx)
	binary.LittleEndian.PutUint16(b[6:8], i.gidIdx)
	binary.LittleEndian.PutUint32(b[8:12], uint32(i.modTime.Unix()))
	binary.LittleEndian.PutUint32(b[12:16], i.index)
	return b
}
func parseInodeHeader(b []byte) (*inodeHeader, error) {
	target := inodeHeaderSize
	if len(b) < target {
		return nil, fmt.Errorf("Received only %d bytes instead of minimum %d", len(b), target)
	}
	i := &inodeHeader{
		inodeType: inodeType(binary.LittleEndian.Uint16(b[0:2])),
		mode:      os.FileMode(binary.LittleEndian.Uint16(b[2:4])),
		uidIdx:    binary.LittleEndian.Uint16(b[4:6]),
		gidIdx:    binary.LittleEndian.Uint16(b[6:8]),
		modTime:   time.Unix(int64(binary.LittleEndian.Uint32(b[8:12])), 0),
		index:     binary.LittleEndian.Uint32(b[12:16]),
	}
	return i, nil
}

// blockdata used by files and directories
type blockData struct {
	size       uint32
	compressed bool
}

func (b *blockData) toUint32() uint32 {
	u := b.size
	if !b.compressed {
		u |= (1 << 24)
	}
	return u
}
func parseBlockData(u uint32) *blockData {
	var mask uint32 = 1 << 24
	return &blockData{
		compressed: u&mask != mask,
		size:       u & 0x00ffffff,
	}
}
func parseFileBlockSizes(b []byte, fileSize, blocksize int) []*blockData {
	count := fileSize / blocksize
	blocks := make([]*blockData, 0)
	for j := 0; j < count && j < len(b); j += 4 {
		blocks = append(blocks, parseBlockData(binary.LittleEndian.Uint32(b[j:j+4])))
	}
	return blocks
}

/*
  All of our 14 inode types
*/
// basicDirectory
type basicDirectory struct {
	startBlock       uint32
	links            uint32
	fileSize         uint16
	offset           uint16
	parentInodeIndex uint32
}

func (i basicDirectory) toBytes() []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b[0:4], i.startBlock)
	binary.LittleEndian.PutUint32(b[4:8], i.links)
	binary.LittleEndian.PutUint16(b[8:10], i.fileSize)
	binary.LittleEndian.PutUint16(b[10:12], i.offset)
	binary.LittleEndian.PutUint32(b[12:16], i.parentInodeIndex)
	return b
}
func (i basicDirectory) size() int64 {
	return int64(i.fileSize)
}
func (i basicDirectory) xattrIndex() (uint32, bool) {
	return 0, false
}
func (i basicDirectory) equal(o inodeBody) bool {
	oi, ok := o.(basicDirectory)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}
func parseBasicDirectory(b []byte) (*basicDirectory, error) {
	target := 16
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes, fewer than minimum %d", len(b), target)
	}
	d := &basicDirectory{
		startBlock:       binary.LittleEndian.Uint32(b[0:4]),
		links:            binary.LittleEndian.Uint32(b[4:8]),
		fileSize:         binary.LittleEndian.Uint16(b[8:10]),
		offset:           binary.LittleEndian.Uint16(b[10:12]),
		parentInodeIndex: binary.LittleEndian.Uint32(b[12:16]),
	}
	return d, nil
}

// extendedDirectory
type extendedDirectory struct {
	links            uint32
	fileSize         uint32
	startBlock       uint32
	parentInodeIndex uint32
	indexCount       uint16
	offset           uint16
	xAttrIndex       uint32
}

func (i extendedDirectory) toBytes() []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], i.fileSize)
	binary.LittleEndian.PutUint32(b[8:12], i.startBlock)
	binary.LittleEndian.PutUint32(b[12:16], i.parentInodeIndex)
	binary.LittleEndian.PutUint16(b[16:18], i.indexCount)
	binary.LittleEndian.PutUint16(b[18:20], i.offset)
	binary.LittleEndian.PutUint32(b[20:24], i.xAttrIndex)
	return b
}
func (i extendedDirectory) size() int64 {
	return int64(i.fileSize)
}
func (i extendedDirectory) xattrIndex() (uint32, bool) {
	return i.xAttrIndex, true
}
func (i extendedDirectory) equal(o inodeBody) bool {
	oi, ok := o.(extendedDirectory)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}

func parseExtendedDirectory(b []byte) (*extendedDirectory, error) {
	target := 24
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes, fewer than minimum %d", len(b), target)
	}
	d := &extendedDirectory{
		links:            binary.LittleEndian.Uint32(b[0:4]),
		fileSize:         binary.LittleEndian.Uint32(b[4:8]),
		startBlock:       binary.LittleEndian.Uint32(b[8:12]),
		parentInodeIndex: binary.LittleEndian.Uint32(b[12:16]),
		indexCount:       binary.LittleEndian.Uint16(b[16:18]),
		offset:           binary.LittleEndian.Uint16(b[18:20]),
		xAttrIndex:       binary.LittleEndian.Uint32(b[20:24]),
	}
	return d, nil
}

// basicFile
type basicFile struct {
	startBlock         uint32 // block count from the start of the data section where data for this file is stored
	fragmentBlockIndex uint32
	fragmentOffset     uint32
	fileSize           uint32
	blockSizes         []*blockData
}

func (i basicFile) equal(o inodeBody) bool {
	oi, ok := o.(basicFile)
	if !ok {
		return false
	}
	if len(i.blockSizes) != len(oi.blockSizes) {
		return false
	}
	for i, b := range i.blockSizes {
		if (b == nil && oi.blockSizes[i] == nil) || (b != nil && oi.blockSizes[i] == nil) {
			return false
		}
		if *b != *oi.blockSizes[i] {
			return false
		}
	}
	return i.startBlock == oi.startBlock && i.fragmentOffset == oi.fragmentOffset && i.fragmentBlockIndex == oi.fragmentBlockIndex && i.fileSize == oi.fileSize
}

func (i basicFile) toBytes() []byte {
	b := make([]byte, 16+4*len(i.blockSizes))
	binary.LittleEndian.PutUint32(b[0:4], i.startBlock)
	binary.LittleEndian.PutUint32(b[4:8], i.fragmentBlockIndex)
	binary.LittleEndian.PutUint32(b[8:12], i.fragmentOffset)
	binary.LittleEndian.PutUint32(b[12:16], i.fileSize)
	for j, e := range i.blockSizes {
		binary.LittleEndian.PutUint32(b[16+j*4:16+j*4+4], e.toUint32())
	}
	return b
}
func (i basicFile) size() int64 {
	return int64(i.fileSize)
}
func (i basicFile) xattrIndex() (uint32, bool) {
	return 0, false
}
func (i basicFile) toExtended() extendedFile {
	return extendedFile{
		startBlock:         uint64(i.startBlock),
		fileSize:           uint64(i.fileSize),
		sparse:             0,
		links:              0,
		fragmentBlockIndex: i.fragmentBlockIndex,
		fragmentOffset:     i.fragmentOffset,
		xAttrIndex:         0,
		blockSizes:         i.blockSizes,
	}
}
func parseBasicFile(b []byte, blocksize int) (*basicFile, error) {
	target := 16
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes, fewer than minimum %d", len(b), target)
	}
	fileSize := binary.LittleEndian.Uint32(b[12:16])
	d := &basicFile{
		startBlock:         binary.LittleEndian.Uint32(b[0:4]),
		fragmentBlockIndex: binary.LittleEndian.Uint32(b[4:8]),
		fragmentOffset:     binary.LittleEndian.Uint32(b[8:12]),
		fileSize:           fileSize,
		blockSizes:         parseFileBlockSizes(b[16:], int(fileSize), blocksize),
	}
	return d, nil
}

// extendedFile
type extendedFile struct {
	startBlock         uint64
	fileSize           uint64
	sparse             uint64
	links              uint32
	fragmentBlockIndex uint32
	fragmentOffset     uint32
	xAttrIndex         uint32
	blockSizes         []*blockData
}

func (i extendedFile) equal(o inodeBody) bool {
	oi, ok := o.(extendedFile)
	if !ok {
		return false
	}
	if len(i.blockSizes) != len(oi.blockSizes) {
		return false
	}
	for i, b := range i.blockSizes {
		if (b == nil && oi.blockSizes[i] == nil) || (b != nil && oi.blockSizes[i] == nil) {
			return false
		}
		if *b != *oi.blockSizes[i] {
			return false
		}
	}
	return i.startBlock == oi.startBlock &&
		i.fragmentOffset == oi.fragmentOffset &&
		i.fragmentBlockIndex == oi.fragmentBlockIndex &&
		i.fileSize == oi.fileSize &&
		i.sparse == oi.sparse &&
		i.links == oi.links &&
		i.xAttrIndex == oi.xAttrIndex
}

func (i extendedFile) toBytes() []byte {
	b := make([]byte, 40+4*len(i.blockSizes))
	binary.LittleEndian.PutUint64(b[0:8], i.startBlock)
	binary.LittleEndian.PutUint64(b[8:16], i.fileSize)
	binary.LittleEndian.PutUint64(b[16:24], i.sparse)
	binary.LittleEndian.PutUint32(b[24:28], i.links)
	binary.LittleEndian.PutUint32(b[28:32], i.fragmentBlockIndex)
	binary.LittleEndian.PutUint32(b[32:36], i.fragmentOffset)
	binary.LittleEndian.PutUint32(b[36:40], i.xAttrIndex)
	for j, e := range i.blockSizes {
		binary.LittleEndian.PutUint32(b[40+j*4:40+j*4+4], e.toUint32())
	}
	return b
}
func (i extendedFile) size() int64 {
	return int64(i.fileSize)
}
func (i extendedFile) xattrIndex() (uint32, bool) {
	return i.xAttrIndex, true
}

func parseExtendedFile(b []byte, blocksize int) (*extendedFile, error) {
	target := 40
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", len(b), target)
	}
	fileSize := binary.LittleEndian.Uint64(b[8:16])
	d := &extendedFile{
		startBlock:         binary.LittleEndian.Uint64(b[0:8]),
		fileSize:           fileSize,
		sparse:             binary.LittleEndian.Uint64(b[16:24]),
		links:              binary.LittleEndian.Uint32(b[24:28]),
		fragmentBlockIndex: binary.LittleEndian.Uint32(b[28:32]),
		fragmentOffset:     binary.LittleEndian.Uint32(b[32:36]),
		xAttrIndex:         binary.LittleEndian.Uint32(b[36:40]),
		blockSizes:         parseFileBlockSizes(b[40:], int(fileSize), blocksize),
	}
	return d, nil
}

// basicSymlink
type basicSymlink struct {
	links  uint32
	target string
}

func (i basicSymlink) readLink() string {
	return target
}

func (i basicSymlink) toBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(i.target)))
	b = append(b, []byte(i.target)...)
	return b
}
func (i basicSymlink) size() int64 {
	return 0
}
func (i basicSymlink) xattrIndex() (uint32, bool) {
	return 0, false
}

func (i basicSymlink) equal(o inodeBody) bool {
	oi, ok := o.(basicSymlink)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}

func parseBasicSymlink(b []byte) (*basicSymlink, error) {
	target := 9
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", len(b), target)
	}
	targetSize := binary.LittleEndian.Uint32(b[4:8])
	if len(b) < 8+int(targetSize) {
		return nil, fmt.Errorf("Received %d bytes which is less than %d header plus indicated target size of %d", len(b), 8, targetSize)
	}
	s := &basicSymlink{
		links:  binary.LittleEndian.Uint32(b[0:4]),
		target: string(b[8 : 8+targetSize]),
	}
	return s, nil
}

// extendedSymlink
type extendedSymlink struct {
	links      uint32
	target     string
	xAttrIndex uint32
}

func (i extendedSymlink) readLink() string {
	return target
}

func (i extendedSymlink) toBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(i.target)))
	b = append(b, []byte(i.target)...)
	b2 := make([]byte, 4)
	binary.LittleEndian.PutUint32(b[0:4], i.xAttrIndex)
	b = append(b, b2...)
	return b
}
func (i extendedSymlink) size() int64 {
	return 0
}
func (i extendedSymlink) xattrIndex() (uint32, bool) {
	return i.xAttrIndex, true
}

func (i extendedSymlink) equal(o inodeBody) bool {
	oi, ok := o.(extendedSymlink)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}

func parseExtendedSymlink(b []byte) (*extendedSymlink, error) {
	target := 13
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", len(b), target)
	}
	targetSize := binary.LittleEndian.Uint32(b[4:8])
	if len(b) < 12+int(targetSize) {
		return nil, fmt.Errorf("Received %d bytes which is less than %d header plus indicated target size of %d", len(b), 12, targetSize)
	}
	s := &extendedSymlink{
		links:      binary.LittleEndian.Uint32(b[0:4]),
		target:     string(b[8 : 8+targetSize]),
		xAttrIndex: binary.LittleEndian.Uint32(b[8+targetSize : 8+targetSize+4]),
	}
	return s, nil
}

type basicDevice struct {
	links uint32
	major uint32
	minor uint32
}

func (i basicDevice) toBytes() []byte {
	b := make([]byte, 8)
	var devNum uint32
	devNum = (i.major << 8) | (i.minor & 0xff) | ((i.minor & 0xfff00) << 12)

	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], devNum)
	return b
}
func (i basicDevice) size() int64 {
	return 0
}
func (i basicDevice) xattrIndex() (uint32, bool) {
	return 0, false
}

func (i basicDevice) equal(o inodeBody) bool {
	oi, ok := o.(basicDevice)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}
func parseBasicDevice(b []byte) (*basicDevice, error) {
	target := 8
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected %d", len(b), target)
	}
	devNum := binary.LittleEndian.Uint32(b[4:8])
	s := &basicDevice{
		links: binary.LittleEndian.Uint32(b[0:4]),
		major: (devNum & 0xfff00) >> 8,
		minor: (devNum & 0xff) | ((devNum >> 12) & 0xfff00),
	}
	return s, nil
}

// basicBlock
type basicBlock basicDevice
type basicChar basicDevice

type extendedDevice struct {
	links      uint32
	major      uint32
	minor      uint32
	xAttrIndex uint32
}

func (i extendedDevice) toBytes() []byte {
	// easiest to use the basic one
	basic := &basicDevice{
		links: i.links,
		major: i.major,
		minor: i.minor,
	}
	b := basic.toBytes()
	b2 := make([]byte, 4)
	binary.LittleEndian.PutUint32(b2[0:4], i.xAttrIndex)
	b = append(b, b2...)
	return b
}
func (i extendedDevice) size() int64 {
	return 0
}
func (i extendedDevice) xattrIndex() (uint32, bool) {
	return i.xAttrIndex, true
}

func (i extendedDevice) equal(o inodeBody) bool {
	oi, ok := o.(extendedDevice)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}

func parseExtendedDevice(b []byte) (*extendedDevice, error) {
	target := 12
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", len(b), target)
	}
	basic, err := parseBasicDevice(b[:8])
	if err != nil {
		return nil, fmt.Errorf("Error parsing block device: %v", err)
	}
	return &extendedDevice{
		links:      basic.links,
		major:      basic.major,
		minor:      basic.minor,
		xAttrIndex: binary.LittleEndian.Uint32(b[8:12]),
	}, nil
}

// extendedBlock
type extendedBlock extendedDevice
type extendedChar extendedDevice

type basicIPC struct {
	links uint32
}

func (i basicIPC) toBytes() []byte {
	b := make([]byte, 4)

	binary.LittleEndian.PutUint32(b[0:4], i.links)
	return b
}
func (i basicIPC) size() int64 {
	return 0
}
func (i basicIPC) xattrIndex() (uint32, bool) {
	return 0, false
}

func (i basicIPC) equal(o inodeBody) bool {
	oi, ok := o.(basicIPC)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}

func parseBasicIPC(b []byte) (*basicIPC, error) {
	target := 4
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected %d", len(b), target)
	}
	s := &basicIPC{
		links: binary.LittleEndian.Uint32(b[0:4]),
	}
	return s, nil
}

type basicFifo basicIPC
type basicSocket basicIPC

type extendedIPC struct {
	links      uint32
	xAttrIndex uint32
}

func (i extendedIPC) toBytes() []byte {
	b := make([]byte, 8)

	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], i.xAttrIndex)
	return b
}
func (i extendedIPC) size() int64 {
	return 0
}
func (i extendedIPC) xattrIndex() (uint32, bool) {
	return i.xAttrIndex, true
}

func (i extendedIPC) equal(o inodeBody) bool {
	oi, ok := o.(extendedIPC)
	if !ok {
		return false
	}
	if i != oi {
		return false
	}
	return true
}

func parseExtendedIPC(b []byte) (*extendedIPC, error) {
	target := 8
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected %d", len(b), target)
	}
	s := &extendedIPC{
		links:      binary.LittleEndian.Uint32(b[0:4]),
		xAttrIndex: binary.LittleEndian.Uint32(b[4:8]),
	}
	return s, nil
}

type extendedFifo extendedIPC
type extendedSocket extendedIPC

// idTable is an indexed table of IDs
type idTable []uint32

func hydrateDirectoryEntries(entries []*directoryEntryRaw, inodes *hashedData, uidgids []uint32, xAttrTable *xAttrTable, blocksize int) ([]*directoryEntry, error) {
	fullEntries := make([]*directoryEntry, 0)
	for _, e := range entries {
		// read the inode for this entry
		inodeData, err := inodes.find(e.startBlock, e.offset)
		if err != nil {
			return nil, fmt.Errorf("Error finding inode for %s: %v", e.name, err)
		}
		// parse the inode
		in, err := parseInode(inodeData, blocksize)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse inode for %s: %v", e.name, err)
		}
		xattrIndex, has := in.body.xattrIndex()
		xattrs := map[string]string{}
		if has && xattrIndex != noXattrFlag {
			xattrs, err = xAttrTable.find(int(xattrIndex))
			if err != nil {
				return nil, fmt.Errorf("Error reading xattrs for %s: %v", e.name, err)
			}
		}
		fullEntries = append(fullEntries, &directoryEntry{
			isSubdirectory: e.isSubdirectory,
			name:           e.name,
			size:           in.body.size(),
			modTime:        in.header.modTime,
			mode:           in.header.mode,
			inode:          in,
			sys: FileStat{
				uid:    uidgids[in.header.uidIdx],
				gid:    uidgids[in.header.gidIdx],
				xattrs: xattrs,
			},
		})
	}
	return fullEntries, nil
}

// parse unknown data
func parseInode(b []byte, blocksize int) (*inodeImpl, error) {
	// must have sufficient data to pass on
	minsize := 17
	if len(b) < minsize {
		return nil, fmt.Errorf("Received %d bytes, insufficient for minimum %d for header and inode", len(b), minsize)
	}
	// first read the header
	header, err := parseInodeHeader(b)
	if err != nil {
		return nil, err
	}
	// now try to read the rest
	i := b[16:]
	var body inodeBody
	switch header.inodeType {
	case inodeBasicDirectory:
		body, err = parseBasicDirectory(i)
	case inodeExtendedDirectory:
		body, err = parseExtendedDirectory(i)
	case inodeBasicFile:
		body, err = parseBasicFile(i, blocksize)
	case inodeExtendedFile:
		body, err = parseExtendedFile(i, blocksize)
	case inodeBasicChar, inodeBasicBlock:
		body, err = parseBasicDevice(i)
	case inodeExtendedChar, inodeExtendedBlock:
		body, err = parseExtendedDevice(i)
	case inodeBasicSymlink:
		body, err = parseBasicSymlink(i)
	case inodeExtendedSymlink:
		body, err = parseExtendedSymlink(i)
	case inodeBasicFifo, inodeBasicSocket:
		body, err = parseBasicIPC(i)
	case inodeExtendedFifo, inodeExtendedSocket:
		body, err = parseExtendedIPC(i)
	}
	if err != nil {
		return nil, fmt.Errorf("Error parsing inode body: %v", err)
	}
	return &inodeImpl{
		header: header,
		body:   body,
	}, nil
}

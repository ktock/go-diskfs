package squashfs

import (
	"encoding/binary"
	"fmt"
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

type inodeHeader struct {
	inodeType inodeType
	uidIdx    uint16
	gidIdx    uint16
	modTime   time.Time
	index     uint32
	// permissions
}

func (i *inodeHeader) toBytes() []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint16(b[0:2], uint16(i.inodeType))
	// PERMISSIONS!!
	binary.LittleEndian.PutUint16(b[4:6], i.uidIdx)
	binary.LittleEndian.PutUint16(b[6:8], i.gidIdx)
	binary.LittleEndian.PutUint32(b[8:12], uint32(i.modTime.Unix()))
	binary.LittleEndian.PutUint32(b[12:16], i.index)
	return b
}
func parseInodeHeader(b []byte) (*inodeHeader, error) {
	target := 16
	if len(b) != target {
		return nil, fmt.Errorf("Received %d bytes instead of expected %d", len(b), target)
	}
	i := &inodeHeader{
		inodeType: inodeType(binary.LittleEndian.Uint16(b[0:2])),
		// PERMISSIONS!!
		uidIdx:  binary.LittleEndian.Uint16(b[4:6]),
		gidIdx:  binary.LittleEndian.Uint16(b[6:8]),
		modTime: time.Unix(int64(binary.LittleEndian.Uint32(b[8:12])), 0),
		index:   binary.LittleEndian.Uint32(b[12:16]),
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
	if b.compressed {
		u |= (1 << 24)
	}
	return u
}
func parseBlockData(u uint32) *blockData {
	var mask uint32 = 1 << 24
	return &blockData{
		compressed: u&mask == mask,
		size:       u & 0x00ffffff,
	}
}

/*
  All of our 14 inode types
*/
// basicDirectory
type basicDirectory struct {
	startBlock       uint32
	links            uint32
	size             uint16
	offset           uint16
	parentInodeIndex uint32
}

func (i *basicDirectory) toBytes() []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b[0:4], i.startBlock)
	binary.LittleEndian.PutUint32(b[4:8], i.links)
	binary.LittleEndian.PutUint16(b[8:10], i.size)
	binary.LittleEndian.PutUint16(b[10:12], i.offset)
	binary.LittleEndian.PutUint32(b[12:16], i.parentInodeIndex)
	return b
}
func parseBasicDirectory(b []byte) (*basicDirectory, error) {
	target := 16
	if len(b) != target {
		return nil, fmt.Errorf("Received %d bytes instead of expected %d", len(b), target)
	}
	d := &basicDirectory{
		startBlock:       binary.LittleEndian.Uint32(b[0:4]),
		links:            binary.LittleEndian.Uint32(b[4:8]),
		size:             binary.LittleEndian.Uint16(b[8:10]),
		offset:           binary.LittleEndian.Uint16(b[10:12]),
		parentInodeIndex: binary.LittleEndian.Uint32(b[12:16]),
	}
	return d, nil
}

// extendedDirectory
type extendedDirectory struct {
	links            uint32
	size             uint32
	startBlock       uint32
	parentInodeIndex uint32
	indexCount       uint16
	offset           uint16
	xAttrIndex       uint32
}

func (i *extendedDirectory) toBytes() []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], i.size)
	binary.LittleEndian.PutUint32(b[8:12], i.startBlock)
	binary.LittleEndian.PutUint32(b[12:16], i.parentInodeIndex)
	binary.LittleEndian.PutUint16(b[16:18], i.indexCount)
	binary.LittleEndian.PutUint16(b[18:20], i.offset)
	binary.LittleEndian.PutUint32(b[20:24], i.xAttrIndex)
	return b
}
func parseExtendedDirectory(b []byte) (*extendedDirectory, error) {
	target := 24
	if len(b) != target {
		return nil, fmt.Errorf("Received %d bytes instead of expected %d", len(b), target)
	}
	d := &extendedDirectory{
		links:            binary.LittleEndian.Uint32(b[0:4]),
		size:             binary.LittleEndian.Uint32(b[4:8]),
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
	startBlock         uint32
	fragmentBlockIndex uint32
	fragmentOffset     uint32
	size               uint32
	blockSizes         []*blockData
}

func (i *basicFile) toBytes() []byte {
	b := make([]byte, 16+4*len(i.blockSizes))
	binary.LittleEndian.PutUint32(b[0:4], i.startBlock)
	binary.LittleEndian.PutUint32(b[4:8], i.fragmentBlockIndex)
	binary.LittleEndian.PutUint32(b[8:12], i.fragmentOffset)
	binary.LittleEndian.PutUint32(b[12:16], i.size)
	for j, e := range i.blockSizes {
		binary.LittleEndian.PutUint32(b[16+j*4:16+j*4+4], e.toUint32())
	}
	return b
}
func parseBasicFile(b []byte) (*basicFile, error) {
	target := 16
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", len(b), target)
	}
	blockBytes := b[16:]
	blocks := make([]*blockData, 0)
	for j := 0; j < len(blockBytes); j++ {
		blocks = append(blocks, parseBlockData(binary.LittleEndian.Uint32(blockBytes[j:j+4])))
	}
	d := &basicFile{
		startBlock:         binary.LittleEndian.Uint32(b[0:4]),
		fragmentBlockIndex: binary.LittleEndian.Uint32(b[4:8]),
		fragmentOffset:     binary.LittleEndian.Uint32(b[8:12]),
		size:               binary.LittleEndian.Uint32(b[12:16]),
		blockSizes:         blocks,
	}
	return d, nil
}

// extendedFile
type extendedFile struct {
	startBlock         uint64
	size               uint64
	sparse             uint64
	links              uint32
	fragmentBlockIndex uint32
	fragmentOffset     uint32
	xAttrIndex         uint32
	blockSizes         []uint32
}

func (i *extendedFile) toBytes() []byte {
	b := make([]byte, 40+4*len(i.blockSizes))
	binary.LittleEndian.PutUint64(b[0:8], i.startBlock)
	binary.LittleEndian.PutUint64(b[8:16], i.size)
	binary.LittleEndian.PutUint64(b[16:24], i.sparse)
	binary.LittleEndian.PutUint32(b[24:28], i.links)
	binary.LittleEndian.PutUint32(b[28:32], i.fragmentBlockIndex)
	binary.LittleEndian.PutUint32(b[32:36], i.fragmentOffset)
	binary.LittleEndian.PutUint32(b[36:40], i.xAttrIndex)
	for j, e := range i.blockSizes {
		binary.LittleEndian.PutUint32(b[40+j*4:40+j*4+4], e)
	}
	return b
}
func parseExtendedFile(b []byte) (*extendedFile, error) {
	target := 40
	if len(b) < target {
		return nil, fmt.Errorf("Received %d bytes instead of expected minimal %d", len(b), target)
	}
	blockBytes := b[40:]
	blocks := make([]uint32, 0)
	for j := 0; j < len(blockBytes); j++ {
		blocks = append(blocks, binary.LittleEndian.Uint32(blockBytes[j:j+4]))
	}
	d := &extendedFile{
		startBlock:         binary.LittleEndian.Uint64(b[0:8]),
		size:               binary.LittleEndian.Uint64(b[8:16]),
		sparse:             binary.LittleEndian.Uint64(b[16:24]),
		links:              binary.LittleEndian.Uint32(b[24:28]),
		fragmentBlockIndex: binary.LittleEndian.Uint32(b[28:32]),
		fragmentOffset:     binary.LittleEndian.Uint32(b[32:36]),
		xAttrIndex:         binary.LittleEndian.Uint32(b[36:40]),
		blockSizes:         blocks,
	}
	return d, nil
}

// basicSymlink
type basicSymlink struct {
	links  uint32
	target string
}

func (i *basicSymlink) toBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(i.target)))
	b = append(b, []byte(i.target)...)
	return b
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

func (i *extendedSymlink) toBytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(i.target)))
	b = append(b, []byte(i.target)...)
	b2 := make([]byte, 4)
	binary.LittleEndian.PutUint32(b[0:4], i.xAttrIndex)
	b = append(b, b2...)
	return b
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

func (i *basicDevice) toBytes() []byte {
	b := make([]byte, 8)
	var devNum uint32
	devNum = (i.major << 8) | (i.minor & 0xff) | ((i.minor & 0xfff00) << 12)

	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], devNum)
	return b
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

func (i *extendedDevice) toBytes() []byte {
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

func (i *basicIPC) toBytes() []byte {
	b := make([]byte, 4)

	binary.LittleEndian.PutUint32(b[0:4], i.links)
	return b
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

func (i *extendedIPC) toBytes() []byte {
	b := make([]byte, 8)

	binary.LittleEndian.PutUint32(b[0:4], i.links)
	binary.LittleEndian.PutUint32(b[4:8], i.xAttrIndex)
	return b
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

package squashfs

import (
	"encoding/binary"
	"fmt"
)

type metadatablock struct {
	compressed bool
	data       []byte
}

const (
	minMetadataBlockSize = 3
)

func getMetadataSize(b []byte) (uint16, bool, error) {
	if len(b) != 2 {
		return 0, false, fmt.Errorf("Cannot read size of metadata block with %d bytes, must have %d", len(b), 2)
	}
	header := binary.LittleEndian.Uint16(b[:2])
	size := header & 0x7fff
	compressed := header&0x8000 != 0x8000
	return size, compressed, nil
}
func parseMetadata(b []byte, c compressor) (*metadatablock, error) {
	if len(b) < minMetadataBlockSize {
		return nil, fmt.Errorf("Metadata block was of len %d, less than minimum %d", len(b), minMetadataBlockSize)
	}
	size, compressed, err := getMetadataSize(b[:2])
	if err != nil {
		return nil, fmt.Errorf("Error reading metadata header: %v", err)
	}
	data := b[2 : 2+size]
	if compressed {
		data, err = c.decompress(data)
		if err != nil {
			return nil, fmt.Errorf("decompress error: %v", err)
		}
	}
	return &metadatablock{
		compressed: compressed,
		data:       data,
	}, nil
}

func (m *metadatablock) toBytes(c compressor) ([]byte, error) {
	b := make([]byte, 2)
	var (
		header uint16
		data   = m.data
		err    error
	)
	if !m.compressed {
		header |= 0x8000
	} else {
		data, err = c.compress(m.data)
		if err != nil {
			return nil, fmt.Errorf("Compression error: %v", err)
		}
	}
	header |= uint16(len(data))
	binary.LittleEndian.PutUint16(b[:2], header)
	copy(b[2:], data)
	return b, nil
}

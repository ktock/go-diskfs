package squashfs

import (
	"encoding/binary"
	"fmt"
	"io"
)

type metadatablock struct {
	compressed bool
	data       []byte
}

const (
	minMetadataBlockSize = 3
)

func getMetadataSize(b []byte) (uint16, bool, error) {
	if len(b) < 2 {
		return 0, false, fmt.Errorf("Cannot read size of metadata block with %d bytes, must have minimum %d", len(b), 2)
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
	if len(b) < int(2+size) {
		return nil, fmt.Errorf("Metadata header said size should be %d but was only %d", size, len(b)-2)
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
	b = append(b, data...)
	return b, nil
}

func readMetaBlock(r io.ReaderAt, c compressor, location int64) ([]byte, uint16, error) {
	// read bytes off the reader to determine how big it is and if compressed
	b := make([]byte, 2)
	r.ReadAt(b, location)
	size, compressed, err := getMetadataSize(b)
	if err != nil {
		return nil, 0, fmt.Errorf("Error getting size and compression for metadata block at %d: %v", location, err)
	}
	b = make([]byte, size)
	read, err := r.ReadAt(b, location+2)
	if err != nil && err != io.EOF {
		return nil, 0, fmt.Errorf("Unable to read metadata block of size %d at location %d: %v", size, location, err)
	}
	if read != len(b) {
		return nil, 0, fmt.Errorf("Read %d instead of expected %d bytes for metadata block at location %d", read, size, location)
	}
	data := b
	if compressed {
		if c == nil {
			return nil, 0, fmt.Errorf("Metadata block at %d compressed, but no compressor provided", location)
		}
		data, err = c.decompress(b)
		if err != nil {
			return nil, 0, fmt.Errorf("decompress error: %v", err)
		}
	}
	return data, size + 2, nil
}

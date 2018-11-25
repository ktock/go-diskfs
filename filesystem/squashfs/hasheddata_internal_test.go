package squashfs

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// func (h *hashedData) find(key uint32, offset uint16) ([]byte, error) {
func TestHashedDataFind(t *testing.T) {
	data := hashedData{
		hash: map[uint32]uint16{
			0: 0, 10: 10, 20: 20,
		},
		data: []byte{
			0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
			20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40,
		},
	}
	tests := []struct {
		key    uint32
		offset uint16
		b      []byte
		err    error
	}{
		{0, 3, data.data[3:], nil},
		{0, 0, data.data[0:], nil},
		{20, 15, data.data[35:], nil},
		{2, 0, nil, fmt.Errorf("Could not find index %d", 2)},
		{0, 100, nil, fmt.Errorf("key %d returns position %d plus offset %d total %d which is greater than data size %d", 0, 0, 100, 100, 42)},
	}

	for i, tt := range tests {
		b, err := data.find(tt.key, tt.offset)
		switch {
		case (err == nil && tt.err != nil) || (err != nil && tt.err == nil) || (err != nil && tt.err != nil && !strings.HasPrefix(err.Error(), tt.err.Error())):
			t.Errorf("%d: mismatched error, actual then expected", i)
			t.Logf("%v", err)
			t.Logf("%v", tt.err)
		case bytes.Compare(b, tt.b) != 0:
			t.Errorf("%d: mismatched result, actual then expected", i)
			t.Logf("% x", b)
			t.Logf("% x", tt.b)
		}

	}
}

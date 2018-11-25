package squashfs

import (
	"bytes"
	"fmt"
)

type hashedData struct {
	hash map[uint32]uint16
	data []byte
}

func (h *hashedData) equal(o *hashedData) bool {
	if o == nil {
		return false
	}
	if len(h.hash) != len(o.hash) {
		return false
	}
	if len(h.data) != len(o.data) {
		return false
	}
	for k, v := range h.hash {
		val, ok := o.hash[k]
		if !ok || val != v {
			return false
		}
	}
	if bytes.Compare(h.data, o.data) != 0 {
		return false
	}
	return true
}
func (h *hashedData) find(key uint32, offset uint16) ([]byte, error) {
	// the key is a position
	pos, ok := h.hash[key]
	if !ok {
		return nil, fmt.Errorf("Could not find index %d", key)
	}
	l := len(h.data)
	if int(pos) > l {
		return nil, fmt.Errorf("key %d returns position %d which is greater than data size %d", key, pos, l)
	}
	totalPos := int(pos + offset)
	if totalPos > l {
		return nil, fmt.Errorf("key %d returns position %d plus offset %d total %d which is greater than data size %d", key, pos, offset, totalPos, l)
	}
	return h.data[totalPos:], nil
}

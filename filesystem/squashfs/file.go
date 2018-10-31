package squashfs

import (
	"fmt"
	"io"
)

// File represents a single file in a squashfs filesystem
//  it is NOT used when working in a workspace, where we just use the underlying OS
type File struct {
	*basicFile
	isReadWrite bool
	isAppend    bool
	offset      int64
	filesystem  *FileSystem
}

// Read reads up to len(b) bytes from the File.
// It returns the number of bytes read and any error encountered.
// At end of file, Read returns 0, io.EOF
// reads from the last known offset in the file from last read or write
// use Seek() to set at a particular point
func (fl *File) Read(b []byte) (int, error) {
	// we have the DirectoryEntry, so we can get the starting location and size
	// squashfs files are *mostly* contiguous, we only need the starting location and size for whole blocks
	// if there are fragments, we need the location of those as well
	fs := fl.filesystem
	size := int(fl.size) - int(fl.offset)
	location := int(fl.startBlock)
	maxRead := size
	file := fs.file

	// if there is nothing left to read, just return EOF
	if size <= 0 {
		return 0, io.EOF
	}

	// we stop when we hit the lesser of
	//   1- len(b)
	//   2- file end
	if len(b) < maxRead {
		maxRead = len(b)
	}

	// just read the requested number of bytes and change our offset
	// figure out which block number has the bytes we are looking for
	startBlock := fl.offset / fs.blocksize
	offset := fl.offset % fs.blocksize
	endBlock := (fl.offset + int64(maxRead)) / fs.blocksize

	// do we end in fragment territory?
	fragments := false
	if endBlock > int64(len(fl.blockSizes)) {
		fragments = true
		endBlock--
	}

	read := 0
	for i := startBlock; i <= endBlock; i++ {
		block := fl.basicFile.blockSizes[i-startBlock]
		input, err := fs.readBlock(i, block.compressed, block.size)
		if err != nil {
			return read, fmt.Errorf("Error reading data block %d from squashfs: %v", i, err)
		}
		copy(b[read:], input)
		read += len(input)
		offset = 0
	}
	// did we have a fragment to read?
	if fragments {
		input, err := fs.readFragment(fl.basicFile.fragmentBlockIndex, fl.basicFile.fragmentOffset, int64(fl.size)%fs.blocksize)
		if err != nil {
			return read, fmt.Errorf("Error reading fragment block %d from squashfs: %v", fl.basicFile.fragmentBlockIndex, err)
		}
		copy(b[read:], input)
	}
	return maxRead, nil
}

// Write writes len(b) bytes to the File.
//  you cannot write to an iso, so this returns an error
func (fl *File) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("Cannot write to a read-only iso filesystem")
}

// Seek set the offset to a particular point in the file
func (fl *File) Seek(offset int64, whence int) (int64, error) {
	newOffset := int64(0)
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekEnd:
		newOffset = int64(fl.size) + offset
	case io.SeekCurrent:
		newOffset = fl.offset + offset
	}
	if newOffset < 0 {
		return fl.offset, fmt.Errorf("Cannot set offset %d before start of file", offset)
	}
	fl.offset = newOffset
	return fl.offset, nil
}

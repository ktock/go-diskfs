package squashfs

import (
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path"

	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/util"
)

const (
	KB               = 1024
	MB               = KB * KB
	defaultBlockSize = 128 * KB
	minBlocksize     = 4 * KB
	maxBlocksize     = 1 * MB
)

// FileSystem implements the FileSystem interface
type FileSystem struct {
	workspace      string
	superblock     *superblock
	size           int64
	start          int64
	file           util.File
	blocksize      int64
	compressor     compressor
	fragmentBlocks []uint64
}

// Equal compare if two filesystems are equal
func (fs *FileSystem) Equal(a *FileSystem) bool {
	localMatch := fs.file == a.file && fs.size == a.size
	superblockMatch := fs.superblock.equal(a.superblock)
	return localMatch && superblockMatch
}

// Workspace get the workspace path
func (fs *FileSystem) Workspace() string {
	return fs.workspace
}

// Create creates a squashfs filesystem in a given directory
//
// requires the util.File where to create the filesystem, size is the size of the filesystem in bytes,
// start is how far in bytes from the beginning of the util.File to create the filesystem,
// and blocksize is is the logical blocksize to use for creating the filesystem
//
// note that you are *not* required to create the filesystem on the entire disk. You could have a disk of size
// 20GB, and create a small filesystem of size 50MB that begins 2GB into the disk.
// This is extremely useful for creating filesystems on disk partitions.
//
// Note, however, that it is much easier to do this using the higher-level APIs at github.com/diskfs/go-diskfs
// which allow you to work directly with partitions, rather than having to calculate (and hopefully not make any errors)
// where a partition starts and ends.
//
// If the provided blocksize is 0, it will use the default of 128 KB.
func Create(f util.File, size int64, start int64, blocksize int64) (*FileSystem, error) {
	if blocksize == 0 {
		blocksize = defaultBlockSize
	}
	// make sure it is an allowed blocksize
	if err := validateBlocksize(blocksize); err != nil {
		return nil, err
	}

	// create a temporary working area where we can create the filesystem.
	//  It is only on `Finalize()` that we write it out to the actual disk file
	tmpdir, err := ioutil.TempDir("", "diskfs_squashfs")
	if err != nil {
		return nil, fmt.Errorf("Could not create working directory: %v", err)
	}

	// create root directory
	// there is nothing in there
	return &FileSystem{
		workspace: tmpdir,
		start:     start,
		size:      size,
		file:      f,
		blocksize: blocksize,
	}, nil
}

// Read reads a filesystem from a given disk.
//
// requires the util.File where to read the filesystem, size is the size of the filesystem in bytes,
// start is how far in bytes from the beginning of the util.File the filesystem is expected to begin,
// and blocksize is is the logical blocksize to use for creating the filesystem
//
// note that you are *not* required to read a filesystem on the entire disk. You could have a disk of size
// 20GB, and a small filesystem of size 50MB that begins 2GB into the disk.
// This is extremely useful for working with filesystems on disk partitions.
//
// Note, however, that it is much easier to do this using the higher-level APIs at github.com/diskfs/go-diskfs
// which allow you to work directly with partitions, rather than having to calculate (and hopefully not make any errors)
// where a partition starts and ends.
//
// If the provided blocksize is 0, it will use the default of 2K bytes
func Read(file util.File, size int64, start int64, blocksize int64) (*FileSystem, error) {
	var (
		read int
		err  error
	)

	if blocksize == 0 {
		blocksize = defaultBlockSize
	}
	// make sure it is an allowed blocksize
	if err = validateBlocksize(blocksize); err != nil {
		return nil, err
	}

	// load the information from the disk

	// read the superblock
	b := make([]byte, superblockSize)
	read, err = file.ReadAt(b, start)
	if err != nil {
		return nil, fmt.Errorf("Unable to read bytes for superblock: %v", err)
	}
	if int64(read) != superblockSize {
		return nil, fmt.Errorf("Read %d bytes instead of expected %d for superblock", read, superblockSize)
	}

	// parse superblock
	s, err := parseSuperblock(b)
	if err != nil {
		return nil, fmt.Errorf("Error parsing superblock: %v", err)
	}

	fs := &FileSystem{
		workspace:  "", // no workspace when we do nothing with it
		start:      start,
		size:       size,
		file:       file,
		superblock: s,
		blocksize:  blocksize,
	}
	return fs, nil
}

// Type returns the type code for the filesystem. Always returns filesystem.TypeFat32
func (fs *FileSystem) Type() filesystem.Type {
	return filesystem.TypeSquashfs
}

// Mkdir make a directory at the given path. It is equivalent to `mkdir -p`, i.e. idempotent, in that:
//
// * It will make the entire tree path if it does not exist
// * It will not return an error if the path already exists
//
// if readonly and not in workspace, will return an error
func (fs *FileSystem) Mkdir(p string) error {
	if fs.workspace == "" {
		return fmt.Errorf("Cannot write to read-only filesystem")
	}
	err := os.MkdirAll(path.Join(fs.workspace, p), 0755)
	if err != nil {
		return fmt.Errorf("Could not create directory %s: %v", p, err)
	}
	// we are not interesting in returning the entries
	return err
}

// ReadDir return the contents of a given directory in a given filesystem.
//
// Returns a slice of os.FileInfo with all of the entries in the directory.
//
// Will return an error if the directory does not exist or is a regular file and not a directory
func (fs *FileSystem) ReadDir(p string) ([]os.FileInfo, error) {
	var fi []os.FileInfo
	var err error
	// non-workspace: read from iso9660
	// workspace: read from regular filesystem
	if fs.workspace != "" {
		fullPath := path.Join(fs.workspace, p)
		// read the entries
		fi, err = ioutil.ReadDir(fullPath)
		if err != nil {
			return nil, fmt.Errorf("Could not read directory %s: %v", p, err)
		}
	} else {
		dirEntries, err := fs.readDirectory(p)
		if err != nil {
			return nil, fmt.Errorf("Error reading directory %s: %v", p, err)
		}
		fi = make([]os.FileInfo, 0, len(dirEntries))
		for _, entry := range dirEntries {
			// ignore any entry that is current directory or parent
			if entry.isSelf || entry.isParent {
				continue
			}
			fi = append(fi, entry)
		}
	}
	return fi, nil
}

// OpenFile returns an io.ReadWriter from which you can read the contents of a file
// or write contents to the file
//
// accepts normal os.OpenFile flags
//
// returns an error if the file does not exist
func (fs *FileSystem) OpenFile(p string, flag int) (filesystem.File, error) {
	var f filesystem.File
	var err error

	// get the path and filename
	dir := path.Dir(p)
	filename := path.Base(p)

	// if the dir == filename, then it is just /
	if dir == filename {
		return nil, fmt.Errorf("Cannot open directory %s as file", p)
	}

	// cannot open to write or append or create if we do not have a workspace
	writeMode := flag&os.O_WRONLY != 0 || flag&os.O_RDWR != 0 || flag&os.O_APPEND != 0 || flag&os.O_CREATE != 0 || flag&os.O_TRUNC != 0 || flag&os.O_EXCL != 0
	if fs.workspace == "" {
		if writeMode {
			return nil, fmt.Errorf("Cannot write to read-only filesystem")
		}

		// get the directory entries
		var entries []*directoryEntry
		entries, err = fs.readDirectory(dir)
		if err != nil {
			return nil, fmt.Errorf("Could not read directory entries for %s", dir)
		}
		// we now know that the directory exists, see if the file exists
		var targetEntry *directoryEntry
		for _, e := range entries {
			eName := e.Name()
			// cannot do anything with directories
			if eName == filename && e.IsDir() {
				return nil, fmt.Errorf("Cannot open directory %s as file", p)
			}
			if eName == filename {
				// if we got this far, we have found the file
				targetEntry = e
				break
			}
		}

		// see if the file exists
		// if the file does not exist, and is not opened for os.O_CREATE, return an error
		if targetEntry == nil {
			return nil, fmt.Errorf("Target file %s does not exist", p)
		}
		// now open the file
		// get the inode for the file
		inode := fs.getInode(targetEntry)

		f = &File{
			basicFile:   inode,
			isReadWrite: false,
			isAppend:    false,
			offset:      0,
			filesystem:  fs,
		}
	} else {
		f, err = os.OpenFile(path.Join(fs.workspace, p), flag, 0644)
		if err != nil {
			return nil, fmt.Errorf("Target file %s does not exist: %v", p, err)
		}
	}

	return f, nil
}

// readDirectory - read directory entry on squashfs only (not workspace)
func (fs *FileSystem) readDirectory(p string) ([]*directoryEntry, error) {
	var (
		location, size uint32
		err            error
		n              int
	)

	// how we do this?
	// 1- use the superblock root inode pointer to read the root inode from the inode table
	// 2- use the root inode to find the location of the root directory data block
	// 3- read the entries in the directory to find the inode for the next level down
	// 4- read the inode for the next level down to get the location of the directory data block
	// 5- repeat steps 3-4 until we find the directory we are looking for
	// 6- read the directory and get the entries

	location, size, err = fs.superblock.rootInode.getLocation(p)
	if err != nil {
		return nil, fmt.Errorf("Unable to read directory tree for %s: %v", p, err)
	}

	// did we still not find it?
	if location == 0 {
		return nil, fmt.Errorf("Could not find directory %s", p)
	}

	// we have a location, let's read the directories from it
	b := make([]byte, size, size)
	n, err = fs.file.ReadAt(b, int64(location)*fs.blocksize)
	if err != nil {
		return nil, fmt.Errorf("Could not read directory entries for %s: %v", p, err)
	}
	if n != int(size) {
		return nil, fmt.Errorf("Reading directory %s returned %d bytes read instead of expected %d", p, n, size)
	}
	// parse the entries
	entries, err := parseDirEntries(b, fs)
	if err != nil {
		return nil, fmt.Errorf("Could not parse directory entries for %s: %v", p, err)
	}
	return entries, nil
}

func (fs *FileSystem) readBlock(location int64, compressed bool, size uint32) ([]byte, error) {
	b := make([]byte, size)
	var data []byte
	read, err := fs.file.ReadAt(b, int64(location)*fs.blocksize)
	if compressed {
		data, err = fs.compressor.decompress(b)
		if err != nil {
			return nil, fmt.Errorf("decompress error: %v", err)
		}
	}
	return data, nil
}

func (fs *FileSystem) readFragment(index, offset uint32, fragmentSize int64) ([]byte, error) {
	// get info from the fragment table
	// figure out which block of the fragment table we need

	// first find where the compressed fragment table entry for the given index is
	lookupOffset := fs.fragmentBlocks[index]
	// figure out the size of the compressed block and if it is compressed
	b := make([]byte, 2)
	read, err := fs.file.ReadAt(b, int64(lookupOffset)*fs.blocksize)
	if err != nil {
		return nil, fmt.Errorf("Unable to read fragment block %d: %v", index, err)
	}
	if read != len(b) {
		return nil, fmt.Errorf("Read %d instead of expected %d bytes for fragment block %d", read, len(b), index)
	}
	// next read that file
	size, compressed, err := getMetadataSize(b[0:2])
	if err != nil {
		return nil, fmt.Errorf("Error getting size and compression for fragment block %d: %v", index, err)
	}
	b = make([]byte, size)
	read, err = fs.file.ReadAt(b, int64(lookupOffset)*fs.blocksize)
	if err != nil {
		return nil, fmt.Errorf("Unable to read fragment block %d: %v", index, err)
	}
	if read != len(b) {
		return nil, fmt.Errorf("Read %d instead of expected %d bytes for fragment block %d", read, len(b), index)
	}
	var data []byte
	if compressed {
		data, err = fs.compressor.decompress(b)
		if err != nil {
			return nil, fmt.Errorf("decompress error: %v", err)
		}
	}
	// we now have the block of the fragment table that contains the information about our fragment
	// look in the table to get the fragment location and size on disk
	// what offset are we in the table?
	fragmentEntryCount := index % fragmentEntriesPerBlock
	tableOffset := fragmentEntryCount * fragmentEntrySize
	// each one is 16 bytes
	fragmentEntryBytes := b[tableOffset : tableOffset+fragmentEntrySize]
	fr, err := parseFragmentEntry(fragmentEntryBytes)
	if err != nil {
		return nil, fmt.Errorf("Error parsing fragment entry: %v", err)
	}
	// now read the fragment block
	data, err = fs.readBlock(int64(fr.start), fr.compressed, fr.size)
	if err != nil {
		return nil, fmt.Errorf("Error reading fragment block from disk: %v", err)
	}
	// now get the data from the offset
	return data[offset:fragmentSize], nil
}

func validateBlocksize(blocksize int64) error {
	blocksizeFloat := float64(blocksize)
	l2 := math.Log2(blocksizeFloat)
	switch {
	case blocksize < minBlocksize:
		return fmt.Errorf("blocksize %d too small, must be at least %d", blocksize, minBlocksize)
	case blocksize > maxBlocksize:
		return fmt.Errorf("blocksize %d too large, must be no more than %d", blocksize, maxBlocksize)
	case math.Trunc(l2) != l2:
		return fmt.Errorf("blocksize %d is not a power of 2", blocksize)
	}
	return nil
}

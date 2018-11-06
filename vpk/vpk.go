//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package vpk

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	vpkSignature  = 0x55aa1234
	vpkHeaderSize = 224
)

type VPKHeader struct {
	Signature, Version    uint32
	TreeSize              uint32
	FileDataSectionSize   uint32
	ArchiveMD5SectionSize uint32
	OtherMD5SectionSize   uint32
	SignatureSectionSize  uint32
}

type VPK struct {
	Header VPKHeader
	Files  map[string]*FileEntry

	f           *os.File
	archives    map[uint16]*os.File
	archiveBase string
}

type FileEntryHeader struct {
	CRC                        uint32
	PreloadBytes, ArchiveIndex uint16
	EntryOffset, EntryLength   uint32
	Terminator                 uint16
}

type FileEntry struct {
	FileEntryHeader
	Name        string
	PreloadData []byte
	TotalSize   int64

	vpk *VPK
}

type Reader interface {
	io.Reader
	io.Seeker
	io.ReaderAt
}

func (e *FileEntry) Open() (Reader, error) {
	if e.EntryLength == 0 {
		return bytes.NewReader(e.PreloadData), nil
	} else if len(e.PreloadData) != 0 {
		return nil, fmt.Errorf("opening vpk file %s: files with both preload and regular data are not supported", e.Name)
	}
	return e.vpk.openSegment(e.ArchiveIndex, e.EntryOffset, e.EntryLength)
}

func readString(b *bufio.Reader) (string, error) {
	buf, err := b.ReadBytes(0)
	if err != nil {
		return "", err
	} else if len(buf) <= 1 {
		return "", io.EOF
	} else if len(buf) == 2 && buf[0] == ' ' && buf[1] == 0 {
		// empty parts are encoded as a single space
		return "", nil
	}
	return string(buf[:len(buf)-1]), nil
}

func Open(vpkName, filterExt string) (*VPK, error) {
	f, err := os.Open(vpkName)
	if err != nil {
		return nil, err
	}
	v := &VPK{
		f:        f,
		archives: make(map[uint16]*os.File),
		Files:    make(map[string]*FileEntry),
	}
	if strings.HasSuffix(vpkName, "_dir.vpk") {
		v.archiveBase, err = filepath.Abs(vpkName[:len(vpkName)-8])
		if err != nil {
			return nil, err
		}
	}
	b := bufio.NewReader(f)
	if err := binary.Read(b, binary.LittleEndian, &v.Header); err != nil {
		return nil, err
	}
	if v.Header.Signature != vpkSignature {
		return nil, errors.New("not a VPK file")
	} else if v.Header.Version != 2 {
		return nil, fmt.Errorf("unsupported VPK version %d", v.Header.Version)
	}
	b.Reset(io.NewSectionReader(f, vpkHeaderSize, int64(v.Header.TreeSize)))
	for {
		fileExt, err := readString(b)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		for {
			dirPath, err := readString(b)
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}
			for {
				filename, err := readString(b)
				if err == io.EOF {
					break
				} else if err != nil {
					return nil, err
				} else if filename == "" {
					break
				}

				var header FileEntryHeader
				if err := binary.Read(b, binary.LittleEndian, &header); err != nil {
					return nil, err
				}
				if filterExt != "" && filterExt != fileExt {
					if _, err := b.Discard(int(header.PreloadBytes)); err != nil {
						return nil, err
					}
					continue
				}
				if dirPath != "" {
					if fileExt != "" {
						filename = dirPath + "/" + filename + "." + fileExt
					} else {
						filename = dirPath + "/" + filename
					}
				} else if fileExt != "" {
					filename += "." + fileExt
				}
				var preload []byte
				if header.PreloadBytes != 0 {
					preload = make([]byte, int(header.PreloadBytes))
					if _, err := io.ReadFull(b, preload); err != nil {
						return nil, err
					}
				}
				v.Files[filename] = &FileEntry{
					FileEntryHeader: header,
					Name:            filename,
					PreloadData:     preload,
					TotalSize:       int64(header.PreloadBytes) + int64(header.EntryLength),
					vpk:             v,
				}
			}
		}
	}
	return v, nil
}

func (v *VPK) Close() error {
	for _, f := range v.archives {
		f.Close()
	}
	v.archives = nil
	v.f.Close()
	return nil
}

func (v *VPK) openSegment(archiveIndex uint16, entryOffset, entryLength uint32) (*io.SectionReader, error) {
	if archiveIndex == 0x7fff {
		// in main vpk
		return io.NewSectionReader(v.f, vpkHeaderSize+int64(v.Header.TreeSize)+int64(entryOffset), int64(entryLength)), nil
	} else if v.archiveBase == "" {
		return nil, errors.New("expected multipart VPK but filename does not end in _dir.vpk")
	}
	arName := fmt.Sprintf("%s_%03d.vpk", v.archiveBase, archiveIndex)
	f := v.archives[archiveIndex]
	if f == nil {
		var err error
		f, err = os.Open(arName)
		if err != nil {
			return nil, err
		}
		v.archives[archiveIndex] = f
	}
	return io.NewSectionReader(f, int64(entryOffset), int64(entryLength)), nil
}

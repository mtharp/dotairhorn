//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package vres

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

const headerVersion = 12

type Resource struct {
	ResourceHeader
	io.Reader
	Blocks   map[string]Block
	DataSize int64
}

type ResourceHeader struct {
	FileSize                uint32
	HeaderVersion, Version  uint16
	BlockOffset, BlockCount uint32
}

type blockHeader struct {
	Type         [4]byte
	Offset, Size uint32
}

type Block struct {
	Data []byte
}

func Parse(r io.ReaderAt, totalSize int64) (*Resource, error) {
	b := bufio.NewReader(io.NewSectionReader(r, 0, 1<<62))
	res := &Resource{Blocks: make(map[string]Block)}
	if err := binary.Read(b, binary.LittleEndian, &res.ResourceHeader); err != nil {
		return nil, err
	}
	if res.HeaderVersion != headerVersion {
		return nil, fmt.Errorf("unknown resource version %d", res.HeaderVersion)
	}
	if res.BlockOffset > 8 {
		if _, err := b.Discard(int(res.BlockOffset) - 8); err != nil {
			return nil, err
		}
	}
	pos := 8 + int64(res.BlockOffset)
	for i := uint32(0); i < res.BlockCount; i++ {
		var bh blockHeader
		if err := binary.Read(b, binary.LittleEndian, &bh); err != nil {
			return nil, err
		}
		buf := make([]byte, int(bh.Size))
		if _, err := r.ReadAt(buf, pos+int64(bh.Offset)); err != nil {
			return nil, err
		}
		res.Blocks[string(bh.Type[:])] = Block{Data: buf}
		pos += 12
	}
	offset := int64(res.FileSize)
	res.DataSize = totalSize - offset
	res.Reader = io.NewSectionReader(r, offset, res.DataSize)
	return res, nil
}

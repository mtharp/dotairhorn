//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package vres

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/mtharp/dotairhorn/internal"
)

type AudioFileType uint32

const (
	TypeAAC AudioFileType = iota
	TypeWAV
	TypeMP3
	TypeUnknown
)

type Sound struct {
	io.Reader
	FileType             AudioFileType
	Format               uint32
	SampleRate, Bits     int
	Channels, SampleSize int
	LoopStart            int32
	Duration             int32
}

type soundHeader struct {
	_          [4]byte
	PackedInfo uint32
	LoopStart  int32
	Duration   int32
}

func (h *soundHeader) unpack(width uint) (v uint32) {
	v = h.PackedInfo & (1<<width - 1)
	h.PackedInfo >>= width
	return
}

func (r *Resource) Sound() (s Sound, err error) {
	block := r.Blocks["DATA"]
	if len(block.Data) < 16 {
		err = errors.New("sound data is missing or truncated")
		return
	}
	var h soundHeader
	err = binary.Read(bytes.NewReader(block.Data), binary.LittleEndian, &h)
	if err != nil {
		return
	}
	s.LoopStart = h.LoopStart
	s.Duration = h.Duration
	s.FileType = AudioFileType(h.unpack(2))
	s.Bits = int(h.unpack(5))
	s.Channels = int(h.unpack(2))
	s.SampleSize = int(h.unpack(3))
	s.Format = h.unpack(2)
	s.SampleRate = int(h.unpack(17))
	if s.FileType == TypeWAV {
		wav := internal.SerializeWaveHeader(s.Channels, s.SampleRate, s.Bits, uint32(r.DataSize))
		s.Reader = io.MultiReader(bytes.NewReader(wav), r.Reader)
	} else {
		s.Reader = r.Reader
	}
	return
}

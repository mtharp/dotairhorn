//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package internal

import (
	"bytes"
	"encoding/binary"
)

const (
	riffMagic  = 0x46464952
	waveFormat = 0x45564157
	formatTag  = 0x20746d66
	dataTag    = 0x61746164
)

type WaveHeader struct {
	// RIFF header
	RiffMagic, RiffSize uint32
	Format              uint32
	// WAVE format chunk
	FormatTag, FormatSize     uint32
	AudioFormat, NumChannels  uint16
	SampleRate, ByteRate      uint32
	BlockAlign, BitsPerSample uint16
	// WAVE data chunk
	DataTag, DataSize uint32
}

func SerializeWaveHeader(channels, rate, bits int, dataLength uint32) []byte {
	h := WaveHeader{
		RiffMagic: riffMagic,
		RiffSize:  dataLength + 42,
		Format:    waveFormat,

		FormatTag:     formatTag,
		FormatSize:    16,
		AudioFormat:   1,
		NumChannels:   uint16(channels),
		SampleRate:    uint32(rate),
		ByteRate:      uint32(rate * channels * bits / 8),
		BlockAlign:    uint16((channels*bits + 7) / 8),
		BitsPerSample: uint16(bits),

		DataTag:  dataTag,
		DataSize: dataLength,
	}
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, h)
	return buf.Bytes()
}

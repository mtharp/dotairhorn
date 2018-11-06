//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package dvoice

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"layeh.com/gopus"
)

type OpusParams struct {
	Channels   int // Number of channels
	SampleRate int // Samples per second
	FrameSize  int // Samples per frame
	BitRate    int // Bits per second
}

func (p OpusParams) FrameTime() time.Duration {
	return time.Second * time.Duration(p.FrameSize) / time.Duration(p.SampleRate)
}

var DefaultParams = OpusParams{
	Channels:   2,
	SampleRate: 48000,
	FrameSize:  960,
	BitRate:    64000,
}

const maxBytes = 1200

func PlayStream(ctx context.Context, opusFrames chan<- []byte, r io.Reader, p OpusParams) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	enc, err := gopus.NewEncoder(p.SampleRate, p.Channels, gopus.Audio)
	if err != nil {
		return err
	}
	enc.SetBitrate(p.BitRate)

	proc := exec.CommandContext(ctx, "ffmpeg",
		"-i", "-",
		"-f", "s16le",
		"-ar", strconv.Itoa(p.SampleRate),
		"-ac", strconv.Itoa(p.Channels),
		"-loglevel", "error",
		"-",
	)
	proc.Stdin = r
	proc.Stderr = os.Stderr
	stdout, err := proc.StdoutPipe()
	if err != nil {
		return err
	}
	if err := proc.Start(); err != nil {
		return err
	}

	pcmbuf := bufio.NewReaderSize(stdout, p.FrameSize*p.Channels*8)
	for {
		pcm := make([]int16, p.FrameSize*p.Channels)
		if err := binary.Read(pcmbuf, binary.LittleEndian, &pcm); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			} else if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		opus, err := enc.Encode(pcm, p.FrameSize, maxBytes)
		if err != nil {
			return err
		}
		select {
		case opusFrames <- opus:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cancel()
	return proc.Wait()
}

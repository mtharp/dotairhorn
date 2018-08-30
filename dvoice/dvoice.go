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
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"
)

const (
	channels   = 2
	sampleRate = 48000
	frameSize  = 960
	maxBytes   = 4096
	bitRate    = 64000
)

func PlayStream(ctx context.Context, vc *discordgo.VoiceConnection, r io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	enc, err := gopus.NewEncoder(sampleRate, channels, gopus.Audio)
	if err != nil {
		return err
	}
	enc.SetBitrate(bitRate)

	proc := exec.CommandContext(ctx, "ffmpeg",
		"-i", "-",
		"-f", "s16le",
		"-ar", strconv.Itoa(sampleRate),
		"-ac", strconv.Itoa(channels),
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

	if err := vc.Speaking(true); err != nil {
		return fmt.Errorf("setting speaking=true: %s", err)
	}
	defer vc.Speaking(false)
	pcmbuf := bufio.NewReaderSize(stdout, frameSize*channels*2*4)
	for {
		pcm := make([]int16, frameSize*channels)
		if err := binary.Read(pcmbuf, binary.LittleEndian, &pcm); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			} else if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		opus, err := enc.Encode(pcm, frameSize, maxBytes)
		if err != nil {
			return err
		}
		if vc.Ready {
			select {
			case vc.OpusSend <- opus:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	cancel()
	return proc.Wait()
}

//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package dvoice

import (
	"context"
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// VoiceHandler makes voice connections to a discord session
type VoiceHandler struct {
	s *discordgo.Session

	mu    sync.Mutex
	conns map[string]*VoiceConn
}

// New creates a new voice handler and attaches it to a discord session
func New(s *discordgo.Session) (h *VoiceHandler) {
	h = &VoiceHandler{
		s:     s,
		conns: make(map[string]*VoiceConn),
	}
	s.AddHandler(h.onVoiceStateUpdate)
	s.AddHandler(h.onVoiceServerUpdate)
	return
}

// Join inititates a connection to a voice channel
func (h *VoiceHandler) Join(guildID, channelID string, p OpusParams) (*VoiceConn, error) {
	frameTime := p.FrameTime()
	if frameConfigs[frameTime] == 0 {
		return nil, fmt.Errorf("frame time %s must be 2.5ms, 5ms, 10ms, or 20ms", frameTime)
	}
	h.mu.Lock()
	if vc := h.conns[guildID]; vc != nil {
		vc.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	vc := &VoiceConn{
		OpusSend:   make(chan []byte, 16),
		h:          h,
		userID:     h.s.State.User.ID,
		guildID:    guildID,
		ctx:        ctx,
		cancel:     cancel,
		opusParams: p,
	}
	h.conns[guildID] = vc
	h.mu.Unlock()
	err := h.s.ChannelVoiceJoinManual(guildID, channelID, false, false)
	return vc, err
}

// LeaveVoice leaves the current voice channel for the specified guild
func (h *VoiceHandler) LeaveVoice(guildID string) error {
	return h.s.ChannelVoiceJoinManual(guildID, "", false, false)
}

// dispatch voice state to the relevant VoiceConn
func (h *VoiceHandler) onVoiceStateUpdate(s *discordgo.Session, st *discordgo.VoiceStateUpdate) {
	h.mu.Lock()
	vc := h.conns[st.GuildID]
	h.mu.Unlock()
	if vc != nil {
		vc.onVoiceStateUpdate(st)
	}
}

// dispatch voice server to the relevant VoiceConn
func (h *VoiceHandler) onVoiceServerUpdate(s *discordgo.Session, st *discordgo.VoiceServerUpdate) {
	h.mu.Lock()
	vc := h.conns[st.GuildID]
	h.mu.Unlock()
	if vc != nil {
		vc.onVoiceServerUpdate(st)
	}
}

type voiceChannelJoinData struct {
	GuildID   *string `json:"guild_id"`
	ChannelID *string `json:"channel_id"`
	SelfMute  bool    `json:"self_mute"`
	SelfDeaf  bool    `json:"self_deaf"`
}

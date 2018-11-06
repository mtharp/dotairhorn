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
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// VoiceConn holds a connection to a single voice channel
type VoiceConn struct {
	// OpusSend consumes frames of encoded opus data and transmits them
	OpusSend chan []byte

	h          *VoiceHandler
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	wsConn     *websocket.Conn
	udpConn    net.Conn
	opusParams OpusParams

	userID    string
	guildID   string
	channelID string
	sessionID string
	token     string
	endpoint  string
	ssrc      uint32

	heartbeatInterval time.Duration
	wsOpen            bool
	heartbeatOnce     sync.Once
	discoverOnce      sync.Once
	senderOnce        sync.Once
}

func (c *VoiceConn) onVoiceStateUpdate(st *discordgo.VoiceStateUpdate) {
	if st.UserID != c.userID || st.ChannelID == "" {
		return
	}
	c.mu.Lock()
	c.channelID = st.ChannelID
	c.sessionID = st.SessionID
	if err := c.openLocked(); err != nil {
		log.Printf("error joining voice channel: %s", err)
	}
	c.mu.Unlock()
}

func (c *VoiceConn) onVoiceServerUpdate(st *discordgo.VoiceServerUpdate) {
	c.mu.Lock()
	c.token = st.Token
	c.endpoint = st.Endpoint
	if err := c.openLocked(); err != nil {
		log.Printf("error joining voice channel: %s", err)
	}
	c.mu.Unlock()
}

func (c *VoiceConn) GuildID() string {
	return c.guildID
}

func (c *VoiceConn) ChannelID() string {
	c.mu.Lock()
	v := c.channelID
	c.mu.Unlock()
	return v
}

func (c *VoiceConn) openLocked() error {
	if c.sessionID == "" || c.endpoint == "" {
		// postpone until both messages have been received
		return nil
	}
	if c.wsOpen {
		log.Printf("warning: VoiceConn.openLocked called twice")
		return nil
	}
	dest := "wss://" + strings.TrimSuffix(c.endpoint, ":80") + "?v=3"
	log.Printf("voice: connecting to endpoint %s", dest)
	var err error
	c.wsConn, _, err = websocket.DefaultDialer.Dial(dest, nil)
	if err != nil {
		return errors.Wrap(err, "connecting to voice endpoint")
	}
	req := wsRequest{
		Op: opIdentify,
		Data: identPayload{
			ServerID:  c.guildID,
			UserID:    c.userID,
			SessionID: c.sessionID,
			Token:     c.token,
		},
	}
	if err := c.wsConn.WriteJSON(req); err != nil {
		return errors.Wrap(err, "sending ident request")
	}
	c.wsOpen = true
	go c.receiveWS()
	return nil
}

// receive websocket events
func (c *VoiceConn) receiveWS() {
	defer c.uncleanClose()
	for c.ctx.Err() == nil {
		// use the heartbeat interval as the basis for a read timeout
		timeout := c.heartbeatInterval * 2
		if timeout == 0 {
			timeout = 120 * time.Second
		}
		if err := c.wsConn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			log.Printf("error: setting read deadline on voice socket: %s", err)
			return
		}
		_, msg, err := c.wsConn.ReadMessage()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("error: receiving from voice socket: %s", err)
			return
		}
		if err := c.onMessage(msg); err != nil {
			log.Printf("warning: parsing message from voice socket: %s", err)
		}
	}
}

// parse and dispatch websocket events
func (c *VoiceConn) onMessage(msg []byte) error {
	var e wsEvent
	if err := json.Unmarshal(msg, &e); err != nil {
		return err
	}
	log.Printf("voice socket: received op %d seq %d type %s", e.Op, e.Seq, e.Type)
	switch e.Op {
	case opReady:
		var p readyPayload
		if err := json.Unmarshal(e.Data, &p); err != nil {
			return err
		}
		go c.discoverOnce.Do(func() {
			if err := c.selectProtocol(p); err != nil {
				log.Printf("error: establishing UDP session: %s", err)
				c.uncleanClose()
			}
		})
	case opSessionDescription:
		var p sessionPayload
		if err := json.Unmarshal(e.Data, &p); err != nil {
			return err
		}
		go c.senderOnce.Do(func() {
			if err := c.opusSender(p.SecretKey); err != nil {
				log.Printf("error: opus sender failed: %s", err)
			}
			c.Close()
		})
	case opHello:
		p := new(struct {
			HeartbeatInterval uint32 `json:"heartbeat_interval"`
		})
		if err := json.Unmarshal(e.Data, p); err != nil {
			return err
		}
		c.mu.Lock()
		c.heartbeatInterval = time.Duration(p.HeartbeatInterval) * time.Millisecond
		c.mu.Unlock()
		go c.heartbeatOnce.Do(c.heartbeatWS)
	}
	return nil
}

// determine our public IP address and select a protocol
func (c *VoiceConn) selectProtocol(p readyPayload) (err error) {
	var selectedMode string
	for _, mode := range p.Modes {
		if mode == modeSalsaPoly {
			selectedMode = mode
		}
	}
	if selectedMode == "" {
		return errors.New("no supported encryption mode")
	}
	addr := fmt.Sprintf("%s:%d", p.IP, p.Port)
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return errors.Wrapf(err, "invalid UDP endpoint %s", addr)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	// discover our IP address
	var myAddr net.UDPAddr
	for i := 0; i < 5; i++ {
		myAddr, err = discoverIP(conn, p.SSRC)
		if err == nil {
			break
		}
		log.Printf("warning: discovering UDP address: %s", err)
		time.Sleep(time.Second)
	}
	if myAddr.IP == nil {
		return errors.New("timed out while trying to discover IP address")
	}
	// select a protocol
	c.mu.Lock()
	req := wsRequest{
		Op: opSelectProtocol,
		Data: selectProtocolPayload{
			Protocol: "udp",
			Data: selectProtocolData{
				Address: myAddr.IP.String(),
				Port:    uint16(myAddr.Port),
				Mode:    modeSalsaPoly,
			},
		},
	}
	err = c.wsConn.WriteJSON(req)
	c.ssrc = p.SSRC
	c.udpConn = conn
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// send periodic heartbeats to keep the websocket alive
func (c *VoiceConn) heartbeatWS() {
	defer c.uncleanClose()
	t := time.NewTicker(c.heartbeatInterval * 3 / 4)
	defer t.Stop()
	for c.ctx.Err() == nil {
		log.Printf("voice heartbeat")
		c.mu.Lock()
		r := wsRequest{
			Op:   opHeartbeat,
			Data: time.Now().Unix(),
		}
		err := c.wsConn.WriteJSON(r)
		c.mu.Unlock()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("error: writing to voice socket: %s", err)
			return
		}
		select {
		case <-t.C:
		case <-c.ctx.Done():
		}
	}
}

func (c *VoiceConn) sendSpeaking(speaking bool) error {
	log.Printf("speaking: %t", speaking)
	c.mu.Lock()
	req := wsRequest{
		Op: opSpeaking,
		Data: speakingPayload{
			Speaking: speaking,
			SSRC:     c.ssrc,
		},
	}
	err := c.wsConn.WriteJSON(req)
	c.mu.Unlock()
	return err
}

// Close terminates the voice connection
func (c *VoiceConn) Close() {
	log.Printf("closing voice connection")
	c.mu.Lock()
	if c.wsOpen {
		c.cancel()
	} else {
		c.closeLocked()
	}
	inChannel := c.channelID != ""
	c.channelID = ""
	c.mu.Unlock()
	if inChannel {
		c.h.LeaveVoice(c.guildID)
	}
}

// tear down the socket immediately. safe to call multiple times.
func (c *VoiceConn) uncleanClose() {
	c.mu.Lock()
	c.closeLocked()
	c.mu.Unlock()
	c.cancel()
}

func (c *VoiceConn) closeLocked() {
	if c.wsConn != nil {
		c.wsConn.Close()
	}
	if c.udpConn != nil {
		c.udpConn.Close()
	}
}

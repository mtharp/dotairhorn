//
// Copyright © Michael Tharp <gxti@partiallystapled.com>
//
// This file is distributed under the terms of the MIT License.
// See the LICENSE file at the top of this tree, or if it is missing a copy can
// be found at http://opensource.org/licenses/MIT
//

package dvoice

import "encoding/json"

type wsRequest struct {
	Op   int         `json:"op"`
	Data interface{} `json:"d"`
}

type wsEvent struct {
	Op   int             `json:"op"`
	Seq  int64           `json:"s"`
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

const (
	opIdentify = iota
	opSelectProtocol
	opReady
	opHeartbeat
	opSessionDescription
	opSpeaking
	opHeartbeatAck
	opResume
	opHello
	opResumed
	opClientDisconnect = 13
)

type identPayload struct {
	ServerID  string `json:"server_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Token     string `json:"token"`
}

type readyPayload struct {
	SSRC  uint32   `json:"ssrc"`
	IP    string   `json:"ip"`
	Port  int      `json:"port"`
	Modes []string `json:"modes"`
}

type sessionPayload struct {
	SecretKey [32]byte `json:"secret_key"`
	Mode      string   `json:"mode"`
}

type speakingPayload struct {
	Speaking bool   `json:"speaking"`
	Delay    int    `json:"delay"`
	SSRC     uint32 `json:"ssrc"`
}

type selectProtocolPayload struct {
	Protocol string             `json:"protocol"`
	Data     selectProtocolData `json:"data"`
}

type selectProtocolData struct {
	Address string `json:"address"`
	Port    uint16 `json:"port"`
	Mode    string `json:"mode"`
}

const modeSalsaPoly = "xsalsa20_poly1305"

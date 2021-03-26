package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"eaglesong.dev/dvoice"
	"github.com/bwmarrin/discordgo"
	"github.com/jackc/pgx"
)

var (
	queueCh = make(chan queuedSound, 10)
	db      *pgx.ConnPool
)

const bitrate = 64000

func main() {
	token := os.Getenv("TOKEN")
	if token == "" {
		log.Fatalln("missing required argument")
	}
	cfg, err := pgx.ParseEnvLibpq()
	if err != nil {
		log.Fatalln("error:", err)
	}
	db, err = pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: cfg})
	if err != nil {
		log.Fatalln("error:", err)
	}

	dc, err := discordgo.New(token)
	if err != nil {
		log.Fatalln(err)
	}
	dc.AddHandler(onReady)
	dc.AddHandler(onMessage)
	dc.LogLevel = discordgo.LogInformational
	if err := dc.Open(); err != nil {
		log.Fatalln(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go playQueued(ctx, dc)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	cancel()
	dc.Close()
}

func onReady(s *discordgo.Session, event *discordgo.Ready) {
	status := os.Getenv("STATUS")
	if status == "" {
		return
	}
	statusType, _ := strconv.ParseInt(os.Getenv("STATUS_TYPE"), 10, 0)
	s.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status: status,
		Activities: []*discordgo.Activity{{
			Name: status,
			Type: discordgo.ActivityType(statusType),

			URL: os.Getenv("STATUS_URL"),
		}},
	})
}

func onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	msg := m.ContentWithMentionsReplaced()
	parts := strings.Fields(msg)
	if len(parts) < 2 {
		return
	}
	var table, baseURL string
	switch parts[0] {
	case "!d2":
		table = "dotairhorn"
		baseURL = dotaBase
	case "!tf2":
		table = "tf2"
		baseURL = tfBase
	default:
		return
	}
	channel := voiceChannelForUser(s, m)
	if channel == nil {
		return
	}
	parts = parts[1:]

	message := strings.Join(parts, " ")
	go func() {
		query := `
		SELECT filename FROM ` + table + ` WHERE left(lower(filename), -4) = lower($1)
		UNION ALL
		(SELECT filename FROM ` + table + ` WHERE hero ILIKE $1 ORDER BY random())
		UNION ALL
		(SELECT filename FROM ` + table + `, websearch_to_tsquery($1) query WHERE search @@ query
		ORDER BY $1 ILIKE hero || '%' DESC, $1 ILIKE '%' || message || '%' DESC, ts_rank_cd(search, query) DESC, random())
		LIMIT 1
		`
		// log.Println(query)
		row := db.QueryRow(query, message)
		var filename string
		if err := row.Scan(&filename); err == pgx.ErrNoRows {
			log.Printf("No message found for %s", message)
			return
		} else if err != nil {
			log.Printf("error: %s", err)
			return
		}
		log.Printf("Selected sound %s", filename)

		frameList, err := fetchSound(baseURL, filename)
		if err != nil {
			log.Printf("error: %s", err)
			return
		}

		q := queuedSound{channel, frameList, filename}
		select {
		case queueCh <- q:
		default:
			log.Printf("play queue overflowed")
		}
	}()
}

func voiceChannelForUser(s *discordgo.Session, m *discordgo.MessageCreate) *discordgo.Channel {
	channel, _ := s.State.Channel(m.ChannelID)
	if channel == nil {
		log.Printf("failed to lookup channel %s", m.ChannelID)
		return nil
	}
	guild, _ := s.State.Guild(channel.GuildID)
	if guild == nil {
		log.Printf("failed to lookup guild %s", channel.GuildID)
		return nil
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			channel, _ := s.State.Channel(vs.ChannelID)
			if channel != nil {
				return channel
			}
		}
	}
	log.Printf("user %s is not in a voice channel", m.Author.ID)
	return nil
}

type queuedSound struct {
	channel   *discordgo.Channel
	frameList [][]byte
	filename  string
}

func playQueued(ctx context.Context, s *discordgo.Session) {
	var vc *dvoice.Conn
	var lastCID, lastGID string
	skipGID := make(map[string]struct{})
	h, err := dvoice.New(s, dvoice.Config{Bitrate: bitrate})
	if err != nil {
		log.Fatalln("error: attaching voice handler:", err)
	}
	leaveTimer := time.NewTimer(0)
	defer func() {
		if vc != nil {
			vc.Close()
		}
		leaveTimer.Stop()
	}()
	for {
		var q queuedSound
		select {
		case <-ctx.Done():
			log.Printf("playq: exiting")
			return
		case <-leaveTimer.C:
			if vc != nil {
				log.Printf("playq: leaving")
				vc.Close()
				vc = nil
				lastCID, lastGID = "", ""
			}
			// stop ignoring guilds we were disconnected from once the timer has elapsed
			for gid := range skipGID {
				log.Printf("re-enabling guild %s", gid)
				delete(skipGID, gid)
			}
			continue
		case q = <-queueCh:
		}
		leaveTimer.Stop()
		select {
		case <-leaveTimer.C:
		default:
		}
		if vc == nil || q.channel.GuildID != lastGID || q.channel.ID != lastCID {
			if vc != nil {
				if q.channel.GuildID != lastGID {
					vc.Close()
				}
				vc = nil
			}
			if _, skip := skipGID[q.channel.GuildID]; skip {
				log.Printf("skipping %s due to previously being disconnected from that guild", q.filename)
				leaveTimer.Reset(time.Second)
				continue
			}
			log.Printf("joining")
			var err error
			vc, err = h.Join(ctx, q.channel.GuildID, q.channel.ID)
			if err != nil {
				log.Printf("error: failed to join voice channel %q<%s> on %s: %s", q.channel.Name, q.channel.ID, q.channel.GuildID, err)
				continue
			}
			log.Printf("joined")
			lastGID, lastCID = q.channel.GuildID, q.channel.ID
		}
		log.Printf("playing %s", q.filename)
		for _, frame := range q.frameList {
			if err := vc.WriteFrame(ctx, frame); err != nil {
				if ctx.Err() != nil {
					log.Printf("playq: exiting")
					return
				}
				log.Printf("playq: force disconnected")
				vc.Close()
				vc = nil
				// skip all further sounds for this guild in this run
				skipGID[lastGID] = struct{}{}
				break
			}
		}
		leaveTimer.Reset(time.Second)
	}
}

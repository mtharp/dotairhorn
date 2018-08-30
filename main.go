package main

import (
	"bufio"
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mtharp/dotairhorn/dvoice"
	"github.com/mtharp/dotairhorn/vpk"
	"github.com/mtharp/dotairhorn/vres"
)

var (
	queueCh = make(chan queuedSound, 10)
	archive *vpk.VPK
)

func main() {
	token := flag.String("token", "", "")
	dotadir := flag.String("dota-dir", "", "")
	flag.Parse()
	if *token == "" || *dotadir == "" {
		log.Fatalln("missing required argument")
	}
	f, err := os.Open("responses.txt")
	if err != nil {
		log.Fatalln(err)
	}
	archive, err = vpk.Open(filepath.Join(*dotadir, "pak01_dir.vpk"), "vsnd_c")
	if err != nil {
		log.Fatalln(err)
	}
	basenames := make(map[string]string, len(archive.Files))
	for name := range archive.Files {
		bn := path.Base(name)
		bn = bn[:len(bn)-7]
		basenames[bn] = name
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		words := strings.Fields(s.Text())
		if len(words) < 2 {
			continue
		}
		// find matching file in vpk
		filename := words[0]
		filename = basenames[filename]
		if filename == "" {
			log.Printf("sound not found in archive: %q", words[0])
			continue
		}
		for _, word := range words[1:] {
			soundmapAdd(word, filename)
		}
	}
	if s.Err() != nil {
		log.Fatalln(err)
	}
	dc, err := discordgo.New(*token)
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
	s.UpdateStatus(0, "The International 9 Qualifiers")
}

func onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	msg := m.ContentWithMentionsReplaced()
	parts := strings.Fields(msg)
	if len(parts) < 2 || (parts[0] != "!dotahero" && parts[0] != "!d2") {
		return
	}
	channel := voiceChannelForUser(s, m)
	if channel == nil {
		return
	}
	parts = parts[1:]
	filename := soundmapFind(parts)
	if filename == "" {
		log.Printf("no matches: %s", strings.Join(parts, " "))
		return
	}
	entry := archive.Files[filename]
	if entry == nil {
		log.Printf("sound %s is missing", filename)
		return
	}
	q := queuedSound{channel, entry}
	select {
	case queueCh <- q:
	default:
		log.Printf("play queue overflowed")
	}
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
	channel *discordgo.Channel
	entry   *vpk.FileEntry
}

func playQueued(ctx context.Context, s *discordgo.Session) {
	var vc *discordgo.VoiceConnection
	leaveTimer := time.NewTimer(0)
	defer func() {
		if vc != nil {
			vc.Disconnect()
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
				vc.Disconnect()
				vc = nil
			}
			continue
		case q = <-queueCh:
			log.Printf("playq: got sound %#v", q)
		}
		leaveTimer.Stop()
		select {
		case <-leaveTimer.C:
		default:
		}
		if vc == nil || vc.GuildID != q.channel.GuildID || vc.ChannelID != q.channel.ID {
			if vc != nil {
				vc.Disconnect()
				vc = nil
			}
			log.Printf("joining")
			var err error
			vc, err = s.ChannelVoiceJoin(q.channel.GuildID, q.channel.ID, false, false)
			if err != nil {
				log.Printf("error: failed to join voice channel %q<%s> on %s: %s", q.channel.Name, q.channel.ID, q.channel.GuildID, err)
				continue
			}
			log.Printf("joined")
		}
		log.Printf("playing %s", q.entry.Name)
		if err := playEntry(ctx, vc, q.entry); err != nil {
			log.Printf("error playing %s: %s", q.entry.Name, err)
		} else {
			log.Printf("played")
		}
		leaveTimer.Reset(1 * time.Second)
	}
}

func playEntry(ctx context.Context, vc *discordgo.VoiceConnection, entry *vpk.FileEntry) error {
	r, err := entry.Open()
	if err != nil {
		return err
	}
	res, err := vres.Parse(r, entry.TotalSize)
	if err != nil {
		return err
	}
	snd, err := res.Sound()
	if err != nil {
		return err
	}
	return dvoice.PlayStream(ctx, vc, snd)
}

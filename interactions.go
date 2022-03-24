package main

import (
	"log"
	"strings"

	"dotairhorn/internal"

	"github.com/bwmarrin/discordgo"
	"github.com/jackc/pgx"
)

var tables = map[string]string{
	"dota2": "dotairhorn",
	"tf2":   "tf2",
}

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "dota2",
		Description: "Play Dota 2 hero voice lines in your current voice channel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "search",
				Description: "Search for a voice line by text",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "hero",
				Description: "Filter search for voice lines by hero",
			},
		},
	},
	{
		Name:        "tf2",
		Description: "Play Team Fortress 2 voice lines in your current voice channel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "search",
				Description: "Search for a voice line by text",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "merc",
				Description: "Filter search for voice lines by merc",
			},
		},
	},
}

type playStatus int

const (
	statusError playStatus = iota
	statusOK
	statusNotFound
)

type selectedSound struct {
	Status   playStatus
	Hero     string
	Icon     string
	Page     string
	Message  string
	Filename string
}

func onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()
	var channel *discordgo.Channel
	if i.Member != nil {
		channel = findVoiceChannel(s, i.GuildID, i.Member.User.ID)
	}
	if channel == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "\U0001F507 Command must be used while you are in a voice channel",
			},
		})
		return
	}
	game := data.Name
	baseURL := baseURLs[game]
	if baseURL == "" {
		return
	}
	var search, hero string
	for _, opt := range data.Options {
		switch opt.Name {
		case "hero", "merc":
			hero = opt.StringValue()
		case "search":
			search = opt.StringValue()
		}
	}
	selected := selectSound(game, search, hero)
	switch selected.Status {
	case statusOK:
	default:
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "\U0001F507 Something went wrong...",
			},
		})
		return
	case statusNotFound:
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "\U0001F50E Not found",
			},
		})
		return
	}
	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    selected.Hero,
			URL:     internal.PageURL(baseURL, selected.Page),
			IconURL: selected.Icon,
		},
		Description: selected.Message,
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})

	frameList, err := fetchSound(baseURL, selected.Filename)
	if err != nil {
		log.Printf("error: %s", err)
		return
	}

	q := queuedSound{channel, frameList, selected.Filename}
	select {
	case queueCh <- q:
	default:
		log.Printf("play queue overflowed")
		return
	}
}

func selectSound(game, search, hero string) (result selectedSound) {
	var query string
	args := []interface{}{search}
	if hero != "" {
		query = `@select@, websearch_to_tsquery($1) query WHERE search @@ query AND hero ILIKE $2 ORDER BY $1 ILIKE '%' || message || '%' DESC, ts_rank_cd(search, query) DESC, random() LIMIT 1`
		args = append(args, hero)
	} else {
		query = `
		@select@ WHERE left(lower(filename), -4) = lower($1)
		UNION ALL
		(@select@ WHERE hero ILIKE $1 ORDER BY random())
		UNION ALL
		(@select@, websearch_to_tsquery($1) query WHERE search @@ query
		ORDER BY $1 ILIKE hero || '%' DESC, $1 ILIKE '%' || message || '%' DESC, ts_rank_cd(search, query) DESC, random())
		LIMIT 1
		`
	}
	query = strings.ReplaceAll(query, "@select@", "SELECT filename, hero, message FROM "+tables[game])
	row := db.QueryRow(query, args...)
	if err := row.Scan(&result.Filename, &result.Hero, &result.Message); err == pgx.ErrNoRows {
		// not found
		return selectedSound{Status: statusNotFound}
	} else if err != nil {
		log.Printf("error: %s", err)
		return selectedSound{Status: statusError}
	}
	log.Printf("Selected sound %s", result.Filename)
	row = db.QueryRow("SELECT icon_url, page FROM hero_icons WHERE game = $1 AND hero = $2", game, result.Hero)
	if err := row.Scan(&result.Icon, &result.Page); err != nil && err != pgx.ErrNoRows {
		log.Printf("error: %s", err)
	}
	result.Status = statusOK
	return result
}

func findVoiceChannel(s *discordgo.Session, guildID, userID string) *discordgo.Channel {
	guild, err := s.State.Guild(guildID)
	if guild == nil {
		log.Printf("failed to lookup guild %s: %+v", guildID, err)
		return nil
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			channel, _ := s.State.Channel(vs.ChannelID)
			if channel != nil {
				return channel
			}
		}
	}
	log.Printf("user %s is not in a voice channel", userID)
	return nil
}

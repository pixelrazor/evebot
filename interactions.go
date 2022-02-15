package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

var (
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "mute",
			Description: "Temporarily mute a member",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "member",
					Description: "Member to mute",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "duration",
					Description: "Duration to mute. Example: 2h5m",
					Required:    true,
				},
			},
		},
		{
			Name:        "unmute",
			Description: "Unmute a member",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "member",
					Description: "Member to unmute",
					Required:    true,
				},
			},
		},
		{
			Name:        "db", // TODO: subcommands for different sets of data
			Description: "Dump the database",
		},
		{
			Name:        "sinfo",
			Description: "Get server information",
		},
		{
			Name:        "minfo",
			Description: "Get member information",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "member",
					Description: "Member to view info for",
				},
			},
		},
	}
	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"mute":   muteHandler,
		"unmute": unmuteHandler,
		"db":     dbHandler,
		"sinfo":  sinfoHandler,
		"minfo":  minfoHandler,
	}
)

func sinfoHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	iconImage, err := s.GuildIcon(i.GuildID)
	if err != nil {
		fmt.Println("sinfo GuildIcon:", err)
		return
	}
	var buff bytes.Buffer
	err = png.Encode(&buff, iconImage)
	if err != nil {
		fmt.Println("sinfo png.Encode:", err)
		return
	}
	guild, err := Guild(s, i.GuildID)
	if err != nil {
		fmt.Println("sinfo Guild:", err)
		return
	}
	fmt.Println("owner:", guild.OwnerID)
	owner, err := s.User(guild.OwnerID)
	if err != nil {
		fmt.Println("sinfo User:", err)
		return
	}
	voice, text := 0, 0
	for _, v := range guild.Channels {
		switch v.Type {
		case discordgo.ChannelTypeGuildText:
			text++
		case discordgo.ChannelTypeGuildVoice:
			voice++
		}
	}
	created, _ := discordgo.SnowflakeTimestamp(guild.ID)
	emojis := make([]string, 1)
	for _, v := range guild.Emojis {
		emoji := v.MessageFormat() + " "
		if len(emojis[len(emojis)-1]+emoji) > 1024 {
			emojis = append(emojis, emoji)
		} else {
			emojis[len(emojis)-1] += emoji
		}
	}
	mesg := &discordgo.MessageSend{
		Embed: &discordgo.MessageEmbed{
			Title: guild.Name,
			Color: embedColor,
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: "attachment://thumb.png",
			},
			Fields: []*discordgo.MessageEmbedField{
				{"ID", guild.ID, true},
				{"Owner", fmt.Sprintf("%v#%v", owner.Username, owner.Discriminator), true},
				{"Members", fmt.Sprint(guild.MemberCount), true},
				{"Text channels", fmt.Sprint(text), true},
				{"Voice channels", fmt.Sprint(voice), true},
				{"Created at", created.Format("January 2, 2006"), true},
				{"Region", guild.Region, true},
				{"Roles", fmt.Sprint(len(guild.Roles)), true},
			},
		},
		Files: []*discordgo.File{
			{
				Name:   "thumb.png",
				Reader: &buff,
			},
		},
	}
	for i, v := range emojis {
		if i == 0 {
			mesg.Embed.Fields = append(mesg.Embed.Fields, &discordgo.MessageEmbedField{"Custom emojis", v, true})
		} else {
			mesg.Embed.Fields = append(mesg.Embed.Fields, &discordgo.MessageEmbedField{"\u200b", v, true})
		}

	}
	_, err = s.InteractionResponseEdit(s.State.User.ID, i.Interaction, &discordgo.WebhookEdit{
		Embeds: []*discordgo.MessageEmbed{mesg.Embed},
		Files:  mesg.Files,
	})
	if err != nil {
		fmt.Println("interaction error sinfo:", err)
	}
}

func minfoHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	user := i.Member.User
	if len(i.ApplicationCommandData().Options) > 0 {
		user = i.ApplicationCommandData().Options[0].UserValue(s)
	}
	embed, err := uinfo(user, i.GuildID, s)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Error: %v", err),
			},
		})
		return
	}

	s.InteractionResponseEdit(s.State.User.ID, i.Interaction, &discordgo.WebhookEdit{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

func dbHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	mem := i.Member
	isAdmin := false
	for _, v := range mem.Roles {
		if v == adminRole || v == modRole {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You aren't an admin, punk",
			},
		})
		return
	}

	mesg := "```\n"
	joined := repo.GetAllJoin()
	leave := repo.GetAllLeave()
	dates := make([]string, 0)
	for k := range joined {
		dates = append(dates, k)
	}
	sort.Strings(dates)
	for _, date := range dates {
		mesg += fmt.Sprintf("%v: +/- %v/%v\n", date, joined[date], leave[date])
	}
	mesg += "```\n"
	muted := repo.GetAllMuted()
	for id, until := range muted {
		mesg += fmt.Sprintf("<@%v> %v: %v\n", id, id, until)
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: mesg,
			Flags:   1 << 6, // TODO: replace with const when defined
		},
	})
}

func unmuteHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	user := i.ApplicationCommandData().Options[0].UserValue(s)
	err := doUnmute(s, i.Member, user.ID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Error: %v", err),
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("%v was unmuted", user.Mention()),
		},
	})
}

func doUnmute(s *discordgo.Session, requester *discordgo.Member, userID string) error {
	isAdmin := false
	for _, v := range requester.Roles {
		if v == adminRole || v == modRole {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return errors.New("you are not an admin")
	}
	err := s.GuildMemberRoleRemove(guildID, userID, muteRole)
	if err != nil {
		return fmt.Errorf("failed to remove role: %w", err)
	}
	repo.DeleteMuted(userID)
	return nil
}

func muteHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	user := i.ApplicationCommandData().Options[0].UserValue(s)
	dur, err := time.ParseDuration(i.ApplicationCommandData().Options[1].StringValue())
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Invalid duration: %v", err),
			},
		})
	}

	err = doMute(s, i.Member, user.ID, dur)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Error: %v", err),
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("%v was muted (%v)", user.Mention(), dur),
		},
	})
}

// TODO: refactor command handling in general. logic function sshould return errors. Maybe differentiate user vs internal errors for logging?

func doMute(s *discordgo.Session, requester *discordgo.Member, userID string, dur time.Duration) error {
	isAdmin := false
	for _, v := range requester.Roles {
		if v == adminRole || v == modRole {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return errors.New("you are not an admin")
	}

	muteMember(s, userID, dur)
	return nil
}

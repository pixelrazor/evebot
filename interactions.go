package main

import (
	"errors"
	"fmt"
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
	}
	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"mute": muteHandler,
	}
)

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

package main

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

func GuildMember(s *discordgo.Session, guildID, userID string) (*discordgo.Member, error) {
	member, err := s.State.Member(guildID, userID)
	if err != nil {
		return s.GuildMember(guildID, userID)
	}
	return member, nil
}

func Guild(s *discordgo.Session, guildID string) (*discordgo.Guild, error) {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return s.Guild(guildID)
	}
	return guild, nil
}

func Role(s *discordgo.Session, guildID, roleID string) (*discordgo.Role, error) {
	role, err := s.State.Role(guildID, roleID)
	if err != nil {
		log.Println("Role cache miss")
		roles, err := s.GuildRoles(guildID)
		if err != nil {
			return nil, err
		}
		for _, role := range roles {
			if role.ID == roleID {
				return role, nil
			}
		}
		return nil, discordgo.ErrStateNotFound
	}
	return role, nil
}

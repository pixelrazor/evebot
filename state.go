package main

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

func GuildMember(s *discordgo.Session, guildID, userID string) (*discordgo.Member, error) {
	member, err := s.State.Member(guildID, userID)
	if err != nil {
		member, err := s.GuildMember(guildID, userID)
		if err != nil {
			return nil, err
		}
		s.State.MemberAdd(member)
		return member, nil
	}
	return member, nil
}

func Guild(s *discordgo.Session, guildID string) (*discordgo.Guild, error) {
	guild, err := s.State.Guild(guildID)
	if err != nil || guild.OwnerID == "" { // For some reason ownerID isn't set when first cached?
		guild, err := s.Guild(guildID)
		if err != nil {
			return nil, err
		}
		s.State.GuildAdd(guild)
		return guild, nil
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
			err := s.State.RoleAdd(guildID, role)
			if err != nil {
				return nil, err
			}
		}
		return s.State.Role(guildID, roleID)
	}
	return role, nil
}

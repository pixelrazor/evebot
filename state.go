package main

import "github.com/bwmarrin/discordgo"

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

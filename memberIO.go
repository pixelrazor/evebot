package main

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

type InviteInfo struct {
	uses int
	// Info about the invite creator
	name          string
	discriminator string
	id            string
}

type InviteEvent struct {
	cause string
}

func (eb *EveBot) processInviteEvents() {
	for event := range eb.inviteEvents {
		switch event.cause {
		default:
			log.Println("Unknown invite event:", event.cause)
		}
	}
}

func (eb *EveBot) handleInviteCreate() interface{} {
	return func(s *discordgo.Session, ic *discordgo.InviteCreate) {

	}
}

func (eb *EveBot) handleMemberAdd() interface{} {
	return func(s *discordgo.Session, gma *discordgo.GuildMemberAdd) {
		if gma.GuildID != guildID {
			return
		}
		eb.repo.IncrementJoin(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
		until, err := eb.repo.GetMuted(gma.User.ID)
		if err == nil {
			// TODO: reapply muted here
			s.ChannelMessageSend(partyChannel, fmt.Sprintf("%v (%v) is a punk ass mute evader (%v remaining)", gma.User.Mention(), gma.User.ID, time.Until(until)))
			eb.repo.DeleteMuted(gma.User.ID)
		}
		applyRoles(s, permanentRoles)
		invitesLock.Lock()
		defer invitesLock.Unlock()
		ginvites, _ := s.GuildInvites(guildID)
		newInvs := make([]Invite, len(ginvites))
		for i := range ginvites {
			newInvs[i].code = ginvites[i].Code
			if ginvites[i].Inviter != nil {
				newInvs[i].discriminator = ginvites[i].Inviter.Discriminator
				newInvs[i].id = ginvites[i].Inviter.ID
				newInvs[i].name = ginvites[i].Inviter.Username
			} else {
				newInvs[i].discriminator = "?"
				newInvs[i].id = "?"
				newInvs[i].name = "?"
			}
			newInvs[i].uses = ginvites[i].Uses
		}
		for _, new := range newInvs {
			for _, old := range invites {
				if old.code == new.code && old.uses+1 == new.uses {
					_, err := s.ChannelMessageSend(babyChannel, fmt.Sprintf("%v (%v) joined using %v, created by %v#%v (%v) (%v uses)", gma.User.Mention(), gma.User.ID, new.code, new.name, new.discriminator, new.id, new.uses))
					if err != nil {
						fmt.Println("Error sending member join message:", err)
					}
					invites = newInvs
					return
				}
			}
		}
		for _, new := range newInvs {
			found := false
			for _, old := range invites {
				if old.code == new.code {
					found = true
					break
				}
			}
			if !found {
				_, err := s.ChannelMessageSend(babyChannel, fmt.Sprintf("%v (%v) joined using %v, created by %v#%v (%v) (%v uses)", gma.User.Mention(), gma.User.ID, new.code, new.name, new.discriminator, new.id, new.uses))
				if err != nil {
					fmt.Println("Error sending member join message:", err)
				}
				invites = newInvs
				return
			}
		}
		_, err = s.ChannelMessageSend(babyChannel, fmt.Sprintf("Idk how but %v (%v) joined", gma.User.Mention(), gma.User.ID))
		if err != nil {
			fmt.Println("Error sending member join message:", err)
		}

	}
}

func (eb *EveBot) handleMemberRemove() interface{} {
	return func(s *discordgo.Session, gmr *discordgo.GuildMemberRemove) {
		if gmr.GuildID != guildID {
			return
		}
		eb.repo.IncrementLeave(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
		_, err := s.ChannelMessageSend(babyChannel, fmt.Sprintf("%v %v#%v (%v) left", gmr.User.Mention(), gmr.User.Username, gmr.User.Discriminator, gmr.User.ID))
		if err != nil {
			fmt.Println("Error sending member leave message:", err)
		}
	}
}

// TODO: serialize the join and leave processing
func refreshInvites(s *discordgo.Session) {
	for {
		func() {
			invitesLock.Lock()
			defer invitesLock.Unlock()
			ginvites, err := s.GuildInvites(guildID)
			if err != nil {
				fmt.Println("Error getting invites:", err)
				return
			}
			newInvs := make([]Invite, len(ginvites))
			for i := range ginvites {
				newInvs[i].code = ginvites[i].Code
				if ginvites[i].Inviter != nil {
					newInvs[i].discriminator = ginvites[i].Inviter.Discriminator
					newInvs[i].name = ginvites[i].Inviter.Username
					newInvs[i].id = ginvites[i].Inviter.ID
				}
				newInvs[i].uses = ginvites[i].Uses
			}
			invites = newInvs
		}()
		<-time.After(10 * time.Minute)
	}
}

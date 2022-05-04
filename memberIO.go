package main

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

type InviteInfo struct {
	code      string
	uses      int
	channel   string
	expiresAt time.Time
	// Info about the invite creator
	inviterName          string
	inviterDiscriminator string
	inviterID            string
}

func (eb *EveBot) processMemberIOEvents() {
	dgInvites, err := eb.s.GuildInvites(guildID)
	if err != nil {
		log.Fatalln("Failed to fetch invites:", err)
	}
	invites := invitesToMap(dgInvites)
	for event := range eb.memberIOEvents {
		switch event := event.(type) {
		case *discordgo.InviteCreate:
			invites[event.Code] = InviteInfo{
				code:                 event.Code,
				uses:                 event.Uses,
				channel:              event.ChannelID,
				expiresAt:            event.CreatedAt.Add(time.Duration(event.MaxAge) * time.Second),
				inviterName:          event.Inviter.Username,
				inviterDiscriminator: event.Inviter.Discriminator,
				inviterID:            event.Inviter.ID,
			}
		case *discordgo.InviteDelete:
			delete(invites, event.Code)
		case *discordgo.GuildMemberAdd:
			eb.repo.IncrementJoin(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))

			// handle some roles on join
			until, err := eb.repo.GetMuted(event.User.ID)
			if err == nil && time.Now().Before(until) {
				eb.mute(eb.s, event.User.ID, time.Until(until))
			}
			applyRoles(eb.s, permanentRoles)

			dgInvites, err := eb.s.GuildInvites(guildID)
			if err != nil {
				_, err := eb.s.ChannelMessageSend(babyChannel, fmt.Sprintf("Idk how but %v (%v) joined (error %v)", event.User.Mention(), event.User.ID, err))
				if err != nil {
					log.Println("Failed to send join message on invites error:", err)
				}
				break
			}

			currentInvites := invitesToMap(dgInvites)
			var target *InviteInfo
			for code, invite := range invites {
				if invite.expiresAt.After(time.Now()) {
					continue
				}

				currentInvite, ok := currentInvites[code]
				if !ok {
					target = &invite
					break
				}
				if currentInvite.uses > invite.uses {
					target = &currentInvite
					break
				}
			}
			invites = currentInvites

			if target == nil {
				_, err := eb.s.ChannelMessageSend(
					babyChannel,
					fmt.Sprintf("Idk how but %v (%v) joined",
						event.User.Mention(),
						event.User.ID))
				if err != nil {
					log.Println("Error sending unknown member join message:", err)
				}
				break
			}
			_, err = eb.s.ChannelMessageSend(
				babyChannel,
				fmt.Sprintf("%v (%v) joined using %v (<#%v>), created by %v#%v (%v) (%v uses)",
					event.User.Mention(),
					event.User.ID,
					target.code,
					target.channel,
					target.inviterName,
					target.inviterDiscriminator,
					target.inviterID,
					target.uses))
			if err != nil {
				log.Println("Error sending member join message:", err)
			}
		case *discordgo.GuildMemberRemove:
			eb.repo.IncrementLeave(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
			_, err := eb.s.ChannelMessageSend(
				babyChannel,
				fmt.Sprintf("%v %v#%v (%v) left",
					event.User.Mention(),
					event.User.Username,
					event.User.Discriminator,
					event.User.ID))
			if err != nil {
				fmt.Println("Error sending member leave message:", err)
			}
		default:
			log.Printf("Unknown invite event: %T", event)
		}
	}
}

func invitesToMap(dgInvites []*discordgo.Invite) map[string]InviteInfo {
	invites := make(map[string]InviteInfo)
	for _, invite := range dgInvites {
		invites[invite.Code] = InviteInfo{
			code:                 invite.Code,
			uses:                 invite.Uses,
			channel:              invite.Channel.ID,
			expiresAt:            invite.CreatedAt.Add(time.Duration(invite.MaxAge) * time.Second),
			inviterName:          invite.Inviter.Username,
			inviterDiscriminator: invite.Inviter.Discriminator,
			inviterID:            invite.Inviter.ID,
		}
	}
	return invites
}

func (eb *EveBot) handleInviteCreate() interface{} {
	return func(s *discordgo.Session, ic *discordgo.InviteCreate) {
		if ic.GuildID != guildID {
			return
		}
		eb.memberIOEvents <- ic
	}
}

func (eb *EveBot) handleInviteDelete() interface{} {
	return func(s *discordgo.Session, id *discordgo.InviteDelete) {
		if id.GuildID != guildID {
			return
		}
		eb.memberIOEvents <- id
	}
}

func (eb *EveBot) handleMemberAdd() interface{} {
	return func(s *discordgo.Session, gma *discordgo.GuildMemberAdd) {
		/*if gma.GuildID != guildID {
			return
		}

		eb.memberIOEvents <- gma
	}
}

func (eb *EveBot) handleMemberRemove() interface{} {
	return func(s *discordgo.Session, gmr *discordgo.GuildMemberRemove) {
		if gmr.GuildID != guildID {
			return
		}
		eb.memberIOEvents <- gmr
	}
}

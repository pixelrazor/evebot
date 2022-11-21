package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pixelrazor/evebot/icon"
)

var (
	Quotes = []string{
		"Tim to feed",
		"Moving in hels",
		"Makng them screim",
		"Enough fourplay",
		"Hurting is yanny",
		"Dont' be shy",
		"Careful. I'm a bitter.",
		"Beg me to sto",
		"All my axes are ded",
		"These curvs are real",
		"Lites out",
		"Stalk nd secude",
		"Let's sneak a round",
		"The night is my whale",
		"Let Evelynn Bot take over",
		"Laying eggs",
	}
	guildID       = "222402041618628608" // TODO: enforce this bot is only ever in this guild?
	babyChannel   = "456120170017194016" // for people that join/leave
	streamChannel = "329093986482651138" // where streamer updates get posted
	partyChannel  = "519668633316753411" // admin bot channel
	trashCan      = "473964707284254730" // place to post deleted messages
	muteRole      = "282710021559549952"
	adminRole     = "453643015467171851"
	modRole       = "222406937768099840"
	streamerRole  = "328636992999129088"
	embedColor    = 0x8031ce

	invites     []invite
	invitesLock sync.Mutex

	pastMessages  [64]*messageBackup
	pastMesgIndex = 0

	// permanentRoles holds member IDs mapped to a list of roles they should have. These roles are
	// applied if they rejoin the server
	// TODO: create admin api to manage this and add to db
	permanentRoles = map[string][]string{
		//"486817299781648385": {"519753627120828428"},
	}
)

type invite struct {
	uses          int
	code          string
	name          string
	discriminator string
	id            string
}
type messageBackup struct {
	id          string
	channelID   string
	content     string
	username    string
	userID      string
	timestamp   string
	attachments []*discordgo.File
}

func main() {
	memdb := flag.Bool("memdb", false, "Specifying this flag uses an in memory repository instead of a database")
	flag.Parse()
	envKey, ok := os.LookupEnv("EVE_BOT")
	if !ok {
		log.Fatalln("Failed to find EVE_BOT in env")
	}

	var repo DataRepository
	if *memdb {
		fmt.Println("memory")
		repo = NewMemoryRepo()
	} else {
		host, ok := os.LookupEnv("DB_HOST")
		if !ok {
			log.Fatalln("Failed to find DB_HOST in env")
		}
		user, ok := os.LookupEnv("DB_USER")
		if !ok {
			log.Fatalln("Failed to find DB_USER in env")
		}
		pass, ok := os.LookupEnv("DB_PASS")
		if !ok {
			log.Fatalln("Failed to find DB_PASS in env")
		}
		name, ok := os.LookupEnv("DB_NAME")
		if !ok {
			log.Fatalln("Failed to find DB_NAME in env")
		}
		pgr, err := NewPostgresRepo(host, name, user, pass)
		if err != nil {
			log.Fatalln("Failed to connect to postgres:", host, name, user, err)
		}
		defer pgr.Close()
		repo = pgr
	}

	key := "Bot " + envKey
	dg, _ := discordgo.New(key)

	bot := EveBot{
		s:      dg,
		repo:   repo,
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	bot.handlers()

	if err := bot.run(); err != nil {
		log.Fatalln("Failed to start bot:", err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "I'm alive")
	})
	http.ListenAndServe(":8086", nil)

	log.Println("peace out")
}

func applyRoles(s *discordgo.Session, userRoles map[string][]string) {
	for user, roles := range userRoles {
		for _, role := range roles {
			err := s.GuildMemberRoleAdd(guildID, user, role)
			if err != nil {
				log.Println("applyRoles error:", user, role, err)
			}
		}
	}
}

// muteMember applies the muted role, then starts a time to remove the role after d elapses
func (eb *EveBot) muteMember(s *discordgo.Session, u string, d time.Duration) {
	s.GuildMemberRoleAdd(guildID, u, muteRole)
	go eb.mute(s, u, d)
}

func (eb *EveBot) mute(s *discordgo.Session, u string, d time.Duration) {
	// TODO: a bug exists if you mute an already muted user longer than the first mute. the user will be unmuted for the shorted duration
	eb.repo.AddMuted(u, time.Now().Add(d))
	<-time.After(d)
	err := s.GuildMemberRoleRemove(guildID, u, muteRole)
	if err != nil {
		log.Println("Failed to remove mute role:", u, err)
	}
	eb.repo.DeleteMuted(u)
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
			newInvs := make([]invite, len(ginvites))
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

func uinfo(u *discordgo.User, guild string, s *discordgo.Session) (*discordgo.MessageEmbed, error) {
	created, _ := discordgo.SnowflakeTimestamp(u.ID)
	member, err := GuildMember(s, guild, u.ID)
	if err != nil {
		fmt.Println("uinfo GuildMember:", err)
		return nil, err
	}
	join := member.JoinedAt

	roles := ""
	for _, v := range member.Roles {
		role, err := Role(s, guild, v)
		if err != nil {
			log.Println("Failed looking up role in uinfo:", err)
			return nil, err
		}
		roles += role.Name + "\n"
	}
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%v#%v", u.Username, u.Discriminator),
		Color: embedColor,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: u.AvatarURL(""),
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "ID",
				Value:  u.ID,
				Inline: true,
			},
			{
				Name:   "Joined server",
				Value:  join.Format("January 2, 2006"),
				Inline: true,
			},
			{
				Name:   "Joined Discord",
				Value:  created.Format("January 2, 2006"),
				Inline: true,
			},
		},
	}
	if roles != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Roles (%v)", len(member.Roles)),
			Value:  roles,
			Inline: true,
		})
	}
	if member.Nick != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Nickname",
			Value:  member.Nick,
			Inline: true,
		})
	}
	return embed, nil
}

func updateIconAndStatus(s *discordgo.Session, random *rand.Rand) error {
	encodedIcon := fmt.Sprintf("data:image/png;base64,%v", icon.EncodedFiles[icon.Filenames[random.Intn(len(icon.Filenames))]])
	_, err := s.UserUpdate("", encodedIcon)
	if err != nil {
		return err
	}
	return s.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status: Quotes[random.Intn(len(Quotes))],
	})
}

type EveBot struct {
	s      *discordgo.Session
	repo   DataRepository
	random *rand.Rand
}

type EveBotInteraction struct {
	command *discordgo.ApplicationCommand
	handler func(s *discordgo.Session, i *discordgo.InteractionCreate)
}

func commandPermissions(permissions ...int) *int64 {
	var permission int64
	for _, p := range permissions {
		permission |= int64(p)
	}
	return &permission
}

func (eb *EveBot) registeredInteractions() []EveBotInteraction {
	return []EveBotInteraction{
		{
			command: &discordgo.ApplicationCommand{
				Name:                     "mute",
				Description:              "Temporarily mute a member",
				DefaultMemberPermissions: commandPermissions(discordgo.PermissionModerateMembers),
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
			handler: eb.muteHandler,
		},
		{
			command: &discordgo.ApplicationCommand{
				Name:                     "unmute",
				Description:              "Unmute a member",
				DefaultMemberPermissions: commandPermissions(discordgo.PermissionModerateMembers),
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionUser,
						Name:        "member",
						Description: "Member to unmute",
						Required:    true,
					},
				},
			},
			handler: eb.unmuteHandler,
		},
		{
			command: &discordgo.ApplicationCommand{
				Name:                     "db",
				Description:              "Dump the database",
				DefaultMemberPermissions: commandPermissions(discordgo.PermissionModerateMembers),
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionSubCommand,
						Name:        "traffic",
						Description: "Monthly server traffic",
					},
					{
						Type:        discordgo.ApplicationCommandOptionSubCommand,
						Name:        "muted",
						Description: "List muted users",
					},
				},
			},
			handler: eb.dbHandler,
		},
		{
			command: &discordgo.ApplicationCommand{
				Name:        "sinfo",
				Description: "Get server information",
			},
			handler: sinfoHandler,
		},
		{
			command: &discordgo.ApplicationCommand{
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
			handler: minfoHandler,
		},
	}
}

func (eb *EveBot) handlers() {
	interactionHandlers := make(map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate))
	for _, interaction := range eb.registeredInteractions() {
		interactionHandlers[interaction.command.Name] = interaction.handler

	}
	eb.s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if handle, ok := interactionHandlers[i.ApplicationCommandData().Name]; ok {
			handle(s, i)
		}
	})
	eb.s.AddHandler(eb.handleOnReady())
	eb.s.AddHandler(eb.handlePresenceUpdate())
	eb.s.AddHandler(eb.handleMemberAdd())
	eb.s.AddHandler(eb.handleMemberRemove())
	eb.s.AddHandler(eb.handleMessageDelete())
	eb.s.AddHandler(eb.handleMessageCreate())
	eb.s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsGuildPresences
}

func (eb *EveBot) run() error {
	if err := eb.s.Open(); err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	for _, v := range eb.registeredInteractions() {
		_, err := eb.s.ApplicationCommandCreate(eb.s.State.User.ID, guildID, v.command)
		if err != nil {
			return fmt.Errorf("cannot create '%v' command: %w", v.command.Name, err)
		}
	}

	return nil
}

func (eb *EveBot) handleOnReady() interface{} {
	return func(s *discordgo.Session, r *discordgo.Ready) {
		go func() {
			for {
				err := updateIconAndStatus(eb.s, eb.random)
				if err != nil {
					log.Println("Failed to update icon and status:", err)
				}
				<-time.After(30 * time.Minute)
			}
		}()
		go refreshInvites(s)
		muted := eb.repo.GetAllMuted()
		for member, mutedUntil := range muted {
			go eb.mute(s, member, time.Until(mutedUntil))
		}
		applyRoles(s, permanentRoles)
		log.Println("Eve bot is ready")
	}
}

func (eb *EveBot) handlePresenceUpdate() interface{} {
	isStreaming := make(map[string]time.Time)

	return func(s *discordgo.Session, p *discordgo.PresenceUpdate) {
		member, err := GuildMember(s, p.GuildID, p.User.ID)
		if err != nil {
			log.Println("Error getting member for presence update:", err)
			return
		}
		isStreamer := false
		for _, role := range member.Roles {
			if role == streamerRole {
				isStreamer = true
				break
			}
		}
		if !isStreamer {
			return
		}

		for _, activity := range p.Activities {
			if activity.Type == discordgo.ActivityTypeStreaming {
				if isStreaming[p.User.ID].IsZero() || time.Since(isStreaming[p.User.ID]) > 4*time.Hour {
					mesg := activity.Details + "\n"
					_, err := s.ChannelMessageSend(streamChannel, mesg+activity.URL)
					if err != nil {
						log.Println("Error sending stream message:", err)
					}
				}
				isStreaming[p.User.ID] = time.Now()
				break
			}
		}
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
		newInvs := make([]invite, len(ginvites))
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

func (eb *EveBot) handleMessageDelete() interface{} {
	return func(s *discordgo.Session, m *discordgo.MessageDelete) {
		if m.GuildID != guildID {
			return
		}
		for _, v := range pastMessages {
			if v == nil {
				break
			}
			if v.id == m.ID {
				embed := &discordgo.MessageSend{
					Embed: &discordgo.MessageEmbed{
						Title:       "Deleted Message",
						Description: v.content,
						Timestamp:   v.timestamp,
						Color:       embedColor,
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:   "Username",
								Value:  v.username,
								Inline: true,
							},
							{
								Name:   "User ID",
								Value:  v.userID,
								Inline: true,
							},
							{
								Name:   "Channel",
								Value:  "<#" + v.channelID + ">",
								Inline: true,
							},
						},
					},
					Files: v.attachments,
				}
				s.ChannelMessageSendComplex(trashCan, embed)
				return
			}
		}

	}
}

func (eb *EveBot) handleMessageCreate() interface{} {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.Bot {
			return
		}
		if m.GuildID == guildID {
			var imgs []*discordgo.File
			for _, v := range m.Attachments {
				resp, err := http.Get(v.URL)
				if err == nil && resp.StatusCode < 300 {
					data, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						resp.Body.Close()
					}
					buff := bytes.NewBuffer(data)
					imgs = append(imgs, &discordgo.File{Name: v.Filename, Reader: buff})
					resp.Body.Close()
				}
			}
			pastMessages[pastMesgIndex] = &messageBackup{
				id:          m.ID,
				channelID:   m.ChannelID,
				content:     m.Content,
				username:    fmt.Sprintf("%v#%v", m.Author.Username, m.Author.Discriminator),
				userID:      m.Author.ID,
				timestamp:   m.Timestamp.Format("2006-01-02"),
				attachments: imgs,
			}
			pastMesgIndex = (pastMesgIndex + 1) % 64
		}

		if hasEgg, _ := regexp.MatchString("(?i)\\beggs?\\b", m.Content); hasEgg || strings.Contains(m.Content, "🥚") {
			_, err := s.ChannelMessageSend(m.ChannelID, "🥚")
			if err != nil {
				fmt.Println("Error sending egg message:", err)
			}
		}
		if lmode, _ := regexp.MatchString("(?i)light (mode|theme)", m.Content); lmode {
			_, err := s.ChannelMessageSend(m.ChannelID, "It's better in the dark")
			if err != nil {
				fmt.Println("Error sending light mode message:", err)
			}
		}
		args := strings.Fields(m.Content)
		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "items?":
				s.ChannelMessageSend(m.ChannelID, "runic > deathcap > lich bane")
			}
		}
	}
}

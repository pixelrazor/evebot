package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/bwmarrin/discordgo"
)

var (
	random = rand.NewSource(time.Now().UnixNano())
	quotes = []string{
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
	dg            *discordgo.Session // TODO: remove this global

	invites     []invite
	invitesLock sync.Mutex

	pastMessages  [64]*messageBackup
	pastMesgIndex = 0

	isStreaming = make(map[string]time.Time)

	// permanentRoles holds member IDs mapped to a list of roles they should have. These roles are
	// applied if they rejoin the server
	// TODO: create admin api to manage this
	permanentRoles = map[string][]string{
		//"486817299781648385": {"519753627120828428"},
	}
)

type img struct {
	name  string
	image image.Image
}
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

var repo DataRepository

func main() {
	memdb := flag.Bool("memdb", false, "Specifying this flag uses an in memory repository instead of redis")
	flag.Parse()
	envKey, ok := os.LookupEnv("EVE_BOT")
	if !ok {
		log.Fatalln("Failed to find EVE_BOT in env")
	}
	if *memdb {
		fmt.Println("memory")
		repo = NewMemoryRepo()
	} else {
		envRedis, ok := os.LookupEnv("REDIS_HOST")
		if !ok {
			log.Fatalln("Failed to find REDIS_HOST in env")
		}
		rdb := redis.NewClient(&redis.Options{
			Addr: envRedis,
			DB:   0,
		})
		err := rdb.Ping(ctx).Err()
		if err != nil {
			log.Fatalln("Failed to ping redis instance:", envRedis, err)
		}
		repo = NewRedisRepo(rdb)
		defer rdb.Close()
	}
	key := "Bot " + envKey
	dg, _ = discordgo.New(key)
	dg.AddHandler(memberJoin)
	dg.AddHandler(memberLeave)
	dg.AddHandler(messageCreate)
	dg.AddHandler(messageDelete)
	dg.AddHandler(presenceUpdate)
	dg.AddHandler(onReady)
	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMembers | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages)
	if err := dg.Open(); err != nil {
		panic(err)
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sig

	log.Println("peace out")
}

func onReady(s *discordgo.Session, r *discordgo.Ready) {
	go func() {
		for {
			changeBotIcon()
			<-time.After(30 * time.Minute)
		}
	}()
	go refreshInvites()
	muted := repo.GetAllMuted()
	for member, mutedUntil := range muted {
		go mute(s, member, mutedUntil.Sub(time.Now()))
	}
	applyRoles(s, permanentRoles)
	log.Println("we up")
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

func presenceUpdate(s *discordgo.Session, p *discordgo.PresenceUpdate) {
	member, err := s.GuildMember(p.GuildID, p.User.ID)
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
		if activity.Type == discordgo.ActivityTypeStreaming { // p.Game != nil  do i need this?
			log.Println("Presence info:", isStreaming[p.User.ID], activity)
			if isStreaming[p.User.ID].IsZero() || time.Since(isStreaming[p.User.ID]) > 4*time.Hour {
				mesg := activity.Name + "\n"
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

// muteMember applies the muted role, then starts a time to remove the role after d elapses
func muteMember(s *discordgo.Session, u string, d time.Duration) {
	s.GuildMemberRoleAdd(guildID, u, muteRole)
	go mute(s, u, d)
}

func mute(s *discordgo.Session, u string, d time.Duration) {
	repo.AddMuted(u, time.Now().Add(d))
	<-time.After(d)
	err := s.GuildMemberRoleRemove(guildID, u, muteRole)
	if err != nil {
		log.Println("Failed to remove mute role:", u, err)
	}
	repo.DeleteMuted(u)
}

func refreshInvites() {
	for {
		func() {
			invitesLock.Lock()
			defer invitesLock.Unlock()
			ginvites, err := dg.GuildInvites(guildID)
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

func messageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
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
						{"Username", v.username, true},
						{"User ID", v.userID, true},
						{"Channel", "<#" + v.channelID + ">", true},
					},
				},
				Files: v.attachments,
			}
			s.ChannelMessageSendComplex(trashCan, embed)
			return
		}
	}

}
func uinfo(u *discordgo.User, channel, guild string, s *discordgo.Session) {
	created, _ := discordgo.SnowflakeTimestamp(u.ID)
	member, err := s.GuildMember(guild, u.ID)
	if err != nil {
		fmt.Println("uinfo GuildMember:", err)
		return
	}
	join, err := discordgo.Timestamp(member.JoinedAt).Parse()
	if err != nil {
		fmt.Println("uinfo JoinedAt.Parse:", err)
		return
	}
	roleMap := make(map[string]string)
	gRoles, err := s.GuildRoles(guild)
	if err != nil {
		fmt.Println("uinfo GuildRoles:", err)
		return
	}
	for _, v := range gRoles {
		roleMap[v.ID] = v.Name
	}
	roles := ""
	for _, v := range member.Roles {
		roles += roleMap[v] + "\n"
	}
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%v#%v", u.Username, u.Discriminator),
		Color: embedColor,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: u.AvatarURL(""),
		},
		Fields: []*discordgo.MessageEmbedField{
			{"ID", u.ID, true},
			{"Joined server", join.Format("January 2, 2006"), true},
			{"Joined Discord", created.Format("January 2, 2006"), true},
		},
	}
	if roles != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{fmt.Sprintf("Roles (%v)", len(member.Roles)), roles, true})
	}
	if member.Nick != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{"Nickname", member.Nick, true})
	}
	_, err = s.ChannelMessageSendEmbed(channel, embed)
	if err != nil {
		fmt.Println("Error sending uinfo message:", err)
	}
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
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
			timestamp:   string(m.Timestamp),
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
		case "?db":
			mem, _ := s.GuildMember(guildID, m.Author.ID)
			isAdmin := false
			for _, v := range mem.Roles {
				if v == adminRole || v == modRole {
					isAdmin = true
					break
				}
			}
			if !isAdmin {
				s.ChannelMessageSend(m.ChannelID, "Error: You ain't no admin, punk")
				return
			}
			channel, err := s.UserChannelCreate(m.Author.ID)
			if err != nil {
				log.Println("Failed to create DM channel:", m.Author.ID, err)
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
			_, err = s.ChannelMessageSend(channel.ID, mesg)
			if err != nil {
				log.Println("Failed to send db dump:", err)
				return
			}
		case "?mute":
			mem, _ := s.GuildMember(guildID, m.Author.ID)
			isAdmin := false
			for _, v := range mem.Roles {
				if v == adminRole || v == modRole {
					isAdmin = true
					break
				}
			}
			if !isAdmin {
				s.ChannelMessageSend(m.ChannelID, "Error: You ain't no admin, punk")
				return
			}
			if len(args) != 3 {
				s.ChannelMessageSend(m.ChannelID, "Error: need 2 arguments (?mute @user duration) (24h for 1 day, 1h30m for an hour and a half, etc)")
				return
			}
			id := args[1]
			if len(m.Mentions) > 0 {
				id = m.Mentions[0].ID
			}
			t, err := time.ParseDuration(args[2])
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Error: invalid duration (?mute @user duration) (24h for 1 day, 1h30m for an hour and a half, etc)")
				return
			}
			muteMember(s, id, t)
		case "?unmute":
			mem, _ := s.GuildMember(guildID, m.Author.ID)
			isAdmin := false
			for _, v := range mem.Roles {
				if v == adminRole || v == modRole {
					isAdmin = true
					break
				}
			}
			if !isAdmin {
				s.ChannelMessageSend(m.ChannelID, "Error: You ain't no admin, punk")
				return
			}
			if len(args) != 2 {
				s.ChannelMessageSend(m.ChannelID, "Error: need 1 argument (?unmute @user)")
				return
			}
			id := args[1]
			if len(m.Mentions) > 0 {
				id = m.Mentions[0].ID
			}
			s.GuildMemberRoleRemove(guildID, id, muteRole)
			repo.DeleteMuted(id)
		case "?uinfo":
			if len(m.Mentions) == 0 {
				uinfo(m.Author, m.ChannelID, m.GuildID, s)
			}
			for _, v := range m.Mentions {
				uinfo(v, m.ChannelID, m.GuildID, s)
			}
		case "?sinfo":
			iconImage, err := s.GuildIcon(m.GuildID)
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
			guild, err := s.Guild(m.GuildID)
			if err != nil {
				fmt.Println("sinfo Guild:", err)
				return
			}
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
					&discordgo.File{
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
			_, err = s.ChannelMessageSendComplex(m.ChannelID, mesg)
			if err != nil {
				fmt.Println("sinfo message send error:", err)
			}
		}
	}
}
func memberLeave(s *discordgo.Session, gmr *discordgo.GuildMemberRemove) {
	fmt.Println("Member leave:", gmr.Member.Mention(), gmr.GuildID)
	if gmr.GuildID != guildID {
		return
	}
	repo.IncrementLeave(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
	_, err := s.ChannelMessageSend(babyChannel, fmt.Sprintf("%v %v#%v (%v) left", gmr.User.Mention(), gmr.User.Username, gmr.User.Discriminator, gmr.User.ID))
	if err != nil {
		fmt.Println("Error sending member leave message:", err)
	}
}
func memberJoin(s *discordgo.Session, gma *discordgo.GuildMemberAdd) {
	fmt.Println("Member join:", gma.Member.Mention(), gma.GuildID)
	if gma.GuildID != guildID {
		return
	}
	repo.IncrementJoin(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
	until, err := repo.GetMuted(gma.User.ID)
	if err == nil {
		// TODO: reapply muted here
		s.ChannelMessageSend(partyChannel, fmt.Sprintf("%v (%v) is a punk ass mute evader (%v remaining)", gma.User.Mention(), gma.User.ID, time.Until(until)))
		repo.DeleteMuted(gma.User.ID)
	}
	applyRoles(s, permanentRoles)
	invitesLock.Lock()
	defer invitesLock.Unlock()
	ginvites, _ := dg.GuildInvites(guildID)
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
func getPNG() string {
	files, _ := ioutil.ReadDir("./")
	pngs := make([]string, 0)
	for _, v := range files {
		if strings.Contains(v.Name(), ".png") {
			pngs = append(pngs, v.Name())
		}
	}
	file := pngs[random.Int63()%int64(len(pngs))]
	img, err := ioutil.ReadFile(file)
	if err != nil {
		fmt.Println(err)
	}
	contentType := http.DetectContentType(img)
	base64img := base64.StdEncoding.EncodeToString(img)
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64img)
}

func changeBotIcon() {
	_, err := dg.UserUpdate("", "", "", getPNG(), "")
	if err != nil {
		fmt.Println("Error updating bot icon:")
	}
	dg.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status: quotes[random.Int63()%int64(len(quotes))],
	})
}

func changeServerIcon() {
	_, err := dg.GuildEdit(guildID, discordgo.GuildParams{Icon: getPNG()})
	if err != nil {
		fmt.Println("Error updating server icon:")
	}
}

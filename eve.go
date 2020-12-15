package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/boltdb/bolt"

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
	guildID       = "222402041618628608"
	babyChannel   = "456120170017194016"
	streamChannel = "329093986482651138"
	partyChannel  = "519668633316753411"
	trashCan      = "473964707284254730"
	muteRole      = "282710021559549952"
	adminRole     = "453643015467171851"
	modRole       = "222406937768099840"
	streamerRole  = "328636992999129088"
	embedColor    = 0x8031ce
	dg            *discordgo.Session
	invites       []invite
	invitesLock   sync.Mutex
	pastMessages  [64]*messageBackup
	pastMesgIndex = 0
	isStreaming   = make(map[string]time.Time)
	db            *bolt.DB
	rolesToCount  = []string{
		"462045631796609044", // bronze
		"462045629615702016", // silver
		"462045627719745546", // gold
		"462045623852728321", // plat
		"462045622191652864", // diamond
		"462045619083804685", // master
		"462045617661673483", // challenger
		"462045652407418880", // na
		"462045650456936468", // EUNE
		"462045654252912640", // EUW
		"462045645537017866", // JP
		"462045633470267395", // TR
		"462045637123506186", // OCE
		"462045635269492736", // RU
		"462045641221341184", // LAN
		"462045639270727690", // LAS
		"462045643473551363", // KR
		"281773382650036235", // Coach
		"494134797044678657", // Head Coach
		"471016108141314048", // Retired Coach
		"474160195145302027", // Owner
		"453643015467171851", // Admin
		"222406937768099840", // Moderator
	}
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

func main() {
	initDB()
	key := "Bot " + os.Getenv("EVE_BOT")
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
	fmt.Println("peace out")
	db.Close()
}
func onReady(s *discordgo.Session, r *discordgo.Ready) {
	go func() {
		for {
			changeBotIcon()
			<-time.After(30 * time.Minute)
		}
	}()
	go refreshInvites()
	/*go func() {
		for {
			<-time.After(24 * time.Hour)
			changeServerIcon()
		}
	}()*/
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("muted"))
		return b.ForEach(func(k, v []byte) error {
			id := string(k)
			var t time.Time
			err := t.GobDecode(v)
			if err != nil {
				fmt.Println("time.GobDecode err:", err)
			} else {
				now := time.Now()
				if now.Before(t) {
					go mute(s, id, t.Sub(now))
				} else {
					s.GuildMemberRoleRemove(guildID, string(k), muteRole)
					b.Delete(k)
				}
			}
			return nil
		})
	})
	applyRoles(s)
	fmt.Println("we up")
}

func applyRoles(s *discordgo.Session) {
	for user, roles := range permanentRoles {
		for _, role := range roles {
			err := s.GuildMemberRoleAdd(guildID, user, role)
			if err != nil {
				fmt.Println("allpyRoles:", user, role, err)
			}
		}
	}
}

func initDB() {
	var err error
	db, err = bolt.Open("eve.db", 0666, nil)
	if err != nil {
		panic(err)
	}
	db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("leave"))
		tx.CreateBucketIfNotExists([]byte("join"))
		tx.CreateBucketIfNotExists([]byte("muted"))
		return nil
	})
}
func incrementJoin() {
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("join"))
		if b == nil {
			return errors.New("Null bucket?")
		}
		key := []byte(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
		val := b.Get(key)
		if len(val) == 0 {
			val = make([]byte, 4)
			binary.BigEndian.PutUint32(val, uint32(1))
			b.Put(key, val)
		} else {
			current := binary.BigEndian.Uint32(val)
			val = make([]byte, 4)
			binary.BigEndian.PutUint32(val, uint32(current+1))
			b.Put(key, val)
		}
		return nil
	})
}
func incrementLeave() {
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("leave"))
		if b == nil {
			return errors.New("Null bucket?")
		}
		key := []byte(fmt.Sprintf("%v/%02d", time.Now().Year(), time.Now().Month()))
		val := b.Get(key)
		if len(val) == 0 {
			val = make([]byte, 4)
			binary.BigEndian.PutUint32(val, uint32(1))
			b.Put(key, val)
		} else {
			current := binary.BigEndian.Uint32(val)
			val = make([]byte, 4)
			binary.BigEndian.PutUint32(val, uint32(current+1))
			b.Put(key, val)
		}
		return nil
	})
}
func presenceUpdate(s *discordgo.Session, p *discordgo.PresenceUpdate) {
	isStreamer := false
	for _, v := range p.Roles {
		if v == streamerRole {
			isStreamer = true
			break
		}
	}
	if !isStreamer {
		return
	}
	// attempt to filter out multiple presence updates that may happen after stream has started
	if p.Game != nil && p.Game.Type == discordgo.GameTypeStreaming {
		if isStreaming[p.User.ID] == (time.Time{}) || time.Since(isStreaming[p.User.ID]) > 4*time.Hour {
			mesg := p.Game.Name + "\n"
			s.ChannelMessageSend(streamChannel, mesg+p.Game.URL)
		}
		isStreaming[p.User.ID] = time.Now()
	}
}
func muteMember(s *discordgo.Session, u, c string, d time.Duration) {
	s.GuildMemberRoleAdd(guildID, u, muteRole)
	go mute(s, u, d)
}
func mute(s *discordgo.Session, u string, d time.Duration) {
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("muted"))
		t, err := time.Now().Add(d).GobEncode()
		if err != nil {
			fmt.Println("gobencode error:", err)
			return err
		}
		b.Put([]byte(u), t)
		return nil
	})
	<-time.After(d)
	s.GuildMemberRoleRemove(guildID, u, muteRole)
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("muted"))
		b.Delete([]byte(u))
		return nil
	})
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
func idToDate(s int64) time.Time {
	const discordEpoch int64 = 1420070400000
	return time.Unix(((s>>22)+discordEpoch)/1000, 0)
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
	id, err := strconv.ParseInt(u.ID, 10, 64)
	if err != nil {
		fmt.Println("uinfo ParseInt:", err)
		return
	}
	created := idToDate(id)
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
			channel, _ := s.UserChannelCreate(m.Author.ID)
			mesg := ""
			db.View(func(tx *bolt.Tx) error {
				mesg += fmt.Sprintf("%s:\n", "join")
				tx.Bucket([]byte("join")).ForEach(func(k, v []byte) error {
					mesg += fmt.Sprintf("   %s: %v\n", k, binary.BigEndian.Uint32(v))
					return nil
				})
				mesg += fmt.Sprintf("%s:\n", "leave")
				tx.Bucket([]byte("leave")).ForEach(func(k, v []byte) error {
					mesg += fmt.Sprintf("   %s: %v\n", k, binary.BigEndian.Uint32(v))
					return nil
				})
				mesg += fmt.Sprintf("%s:\n", "muted")
				tx.Bucket([]byte("muted")).ForEach(func(k, v []byte) error {
					var t time.Time
					err := t.GobDecode(v)
					if err != nil {
						fmt.Println(".gb dobdecode err:", err)
					}
					mesg += fmt.Sprintf("   <@%s> %s: %v\n", k, k, time.Until(t).Truncate(time.Millisecond))
					return nil
				})
				return nil
			})
			s.ChannelMessageSend(channel.ID, mesg)

		case "?rolecount":
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
			rolecount := make(map[string]int)
			mems, err := s.GuildMembers(guildID, "", 1000)
			for _, mem := range mems {
				for _, v := range mem.Roles {
					rolecount[v]++
				}
			}
			for err != nil && len(mems) == 1000 {
				mems, err = s.GuildMembers(guildID, mems[999].User.ID, 1000)
				for _, mem := range mems {
					for _, v := range mem.Roles {
						rolecount[v]++
					}
				}
			}

			roleNames := make(map[string]string)
			roles, _ := s.GuildRoles(guildID)
			for _, v := range roles {
				roleNames[v.ID] = v.Name
			}
			roleList := make([]struct {
				count int
				name  string
			}, len(rolesToCount))
			for i := range rolesToCount {
				roleList[i] = struct {
					count int
					name  string
				}{rolecount[rolesToCount[i]], roleNames[rolesToCount[i]]}
			}
			mesg := ""
			channel, _ := s.UserChannelCreate(m.Author.ID)
			for _, v := range roleList {
				mesg += fmt.Sprintf("%v: %v\n", v.name, v.count)
				if len(mesg) > 1800 {
					s.ChannelMessageSend(channel.ID, mesg)
					mesg = ""
				}
			}
			s.ChannelMessageSend(channel.ID, mesg)
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
			if len(m.Mentions) == 0 {
				s.ChannelMessageSend(m.ChannelID, "Error: need to mention user to mute (.mute @user duration) (24h for 1 day, 1h30m for an hour and a half, etc)")
				return
			}
			if len(args) != 3 {
				s.ChannelMessageSend(m.ChannelID, "Error: need 2 arguments (.mute @user duration) (24h for 1 day, 1h30m for an hour and a half, etc)")
				return
			}
			t, err := time.ParseDuration(args[2])
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Error: invalid duration (.mute @user duration) (24h for 1 day, 1h30m for an hour and a half, etc)")
				return
			}
			muteMember(s, m.Mentions[0].ID, m.ChannelID, t)
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
			id, err := strconv.ParseInt(guild.ID, 10, 64)
			if err != nil {
				fmt.Println("sinfo ParseInt:", err)
				return
			}
			created := idToDate(id)
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
	go incrementLeave()
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
	go incrementJoin()
	db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("muted"))
		result := b.Get([]byte(gma.User.ID))
		if result != nil {
			var t time.Time
			t.GobDecode(result)
			s.ChannelMessageSend(partyChannel, fmt.Sprintf("%v (%v) is a punk ass mute evader (%v remaining)", gma.User.Mention(), gma.User.ID, time.Until(t)))
			b.Delete([]byte(gma.User.ID))
		}
		return nil
	})
	applyRoles(s)
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
	_, err := s.ChannelMessageSend(babyChannel, fmt.Sprintf("Idk how but %v (%v) joined", gma.User.Mention(), gma.User.ID))
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
	dg.UpdateStatus(0, quotes[random.Int63()%int64(len(quotes))])
}
func changeServerIcon() {
	_, err := dg.GuildEdit(guildID, discordgo.GuildParams{Icon: getPNG()})
	if err != nil {
		fmt.Println("Error updating server icon:")
	}
}

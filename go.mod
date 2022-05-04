module github.com/pixelrazor/evebot

go 1.17

require (
	github.com/bwmarrin/discordgo v0.24.0
	github.com/lib/pq v1.10.4
)

require (
	github.com/gorilla/websocket v1.5.0 // indirect
	golang.org/x/crypto v0.0.0-20220321153916-2c7772ba3064 // indirect
	golang.org/x/sys v0.0.0-20220325203850-36772127a21f // indirect
)

replace (
	github.com/bwmarrin/discordgo => ../discordgo
)
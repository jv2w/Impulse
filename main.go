package main

import (
	"github.com/jv2w/Impulse/server"
	"github.com/jv2w/Impulse/server/player/chat"
	"log/slog"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	chat.Global.Subscribe(chat.StdoutSubscriber{})
	conf, err := userConfig().Config(slog.Default())
	if err != nil {
		panic(err)
	}

	srv := conf.New()
	srv.CloseOnProgramEnd()

	srv.Listen()
	for p := range srv.Accept() {
		_ = p
	}
}

// userConfig returns the settings the server runs with. No file is read or
// written: edit the values below to configure the server. Every field is set
// explicitly to its default so that all available settings are visible here.
func userConfig() server.UserConfig {
	c := server.DefaultConfig()

	// Network.
	c.Network.Address = ":19132"
	c.Network.Transport = nil // "raknet" and/or "nethernet"; empty means RakNet only.
	c.Network.NetherNet.Address = ""
	c.Network.NetherNet.KeyFile = ""
	c.Network.NetherNet.Domain = ""
	c.Network.NetherNet.UDPPorts = ""

	// Server.
	c.Server.Name = "Impulse Server"
	c.Server.AuthEnabled = true
	c.Server.DisableJoinQuitMessages = false
	c.Server.MuteEmoteChat = false

	// World.
	c.World.SaveData = true
	c.World.Folder = "world"

	// Players.
	c.Players.MaxCount = 10
	c.Players.MaximumChunkRadius = 16
	c.Players.SaveData = true
	c.Players.Folder = "players"

	// Resources.
	c.Resources.AutoBuildPack = true
	c.Resources.Folder = "resources"
	c.Resources.Required = false

	return c
}

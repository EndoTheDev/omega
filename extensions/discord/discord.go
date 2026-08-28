// Package discord provides the omega Discord channel seam. It runs a
// Discord bot that relays DMs to the omega HTTP gateway (/chat), mapping
// one omega session per Discord channel so multi-turn DMs keep memory.
package discord

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// Client relays DMs to the omega gateway.
type Client struct {
	token     string
	gateway   string
	byChannel map[string]string
	mu        sync.Mutex
	http      *http.Client
}

// New creates a Discord client bound to the gateway base URL.
func New(token, gateway string) *Client {
	return &Client{
		token:     token,
		gateway:   strings.TrimRight(gateway, "/"),
		byChannel: map[string]string{},
		http:      &http.Client{},
	}
}

// Run connects the bot and blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	dg, err := discordgo.New("Bot " + c.token)
	if err != nil {
		return fmt.Errorf("discord init: %w", err)
	}
	dg.Identify.Intents = discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessages
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		c.onMessage(s, m)
	})
	if err := dg.Open(); err != nil {
		return fmt.Errorf("discord connect: %w", err)
	}
	defer dg.Close()

	if ok, err := c.health(); err != nil || !ok {
		fmt.Printf("omega-discord: connected, but gateway at %s is unreachable: %v\n", c.gateway, err)
	} else {
		fmt.Printf("omega-discord: connected to %s\n", c.gateway)
	}

	<-ctx.Done()
	return nil
}

// onMessage relays a DM to the gateway and posts the reply back to the
// channel it came from. Messages starting with "/" are treated as commands.
func (c *Client) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	if m.GuildID != "" {
		return
	}
	text := strings.TrimSpace(m.Content)
	if text == "" {
		return
	}

	if strings.HasPrefix(text, "/") {
		c.handleCommand(s, m.ChannelID, text)
		return
	}

	sessionID, err := c.ensureSession(m.ChannelID)
	if err != nil {
		c.send(s, m.ChannelID, "discord: "+err.Error())
		return
	}

	reply, err := c.chat(sessionID, text)
	if err != nil {
		c.send(s, m.ChannelID, "discord: "+err.Error())
		return
	}
	c.send(s, m.ChannelID, reply)
}

// handleCommand processes a "/" command for a channel.
func (c *Client) handleCommand(s *discordgo.Session, channelID, text string) {
	fields := strings.Fields(text)
	cmd := fields[0]

	switch cmd {
	case "/new":
		c.mu.Lock()
		var created struct {
			ID string `json:"id"`
		}
		err := c.gatewayPost("/sessions", map[string]any{}, &created)
		if err == nil && created.ID != "" {
			c.byChannel[channelID] = created.ID
		}
		c.mu.Unlock()
		if err != nil {
			c.send(s, channelID, "discord: "+err.Error())
			return
		}
		c.send(s, channelID, "New session started for this channel.")
	case "/sessions":
		if len(fields) > 1 && fields[1] == "delete" {
			if len(fields) < 3 {
				c.send(s, channelID, "Usage: /sessions delete <id>")
				return
			}
			id := fields[2]
			if err := c.gatewayDelete("/sessions/" + id); err != nil {
				c.send(s, channelID, "discord: "+err.Error())
				return
			}
			c.send(s, channelID, "Deleted session "+id)
			return
		}
		var sessions []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		if err := c.gatewayGet("/sessions", &sessions); err != nil {
			c.send(s, channelID, "discord: "+err.Error())
			return
		}
		if len(sessions) == 0 {
			c.send(s, channelID, "No sessions yet.")
			return
		}
		var b strings.Builder
		b.WriteString("Sessions:\n")
		for _, sess := range sessions {
			label := sess.Label
			if label == "" {
				label = "(unnamed)"
			}
			fmt.Fprintf(&b, "- %s (%s)\n", sess.ID, label)
		}
		c.send(s, channelID, strings.TrimSpace(b.String()))
	case "/resume":
		if len(fields) < 2 {
			c.send(s, channelID, "Usage: /resume <session-id>")
			return
		}
		id := fields[1]
		var sess struct {
			ID string `json:"id"`
		}
		if err := c.gatewayGet("/sessions/"+id, &sess); err != nil {
			c.send(s, channelID, "discord: session not found: "+err.Error())
			return
		}
		c.mu.Lock()
		c.byChannel[channelID] = id
		c.mu.Unlock()
		c.send(s, channelID, "Resumed session "+id+" for this channel.")
	case "/model":
		c.send(s, channelID, "Model switching isn't supported over Discord yet — set it in config.yaml and restart.")
	case "/help":
		c.send(s, channelID, "Commands: /new (fresh session), /sessions (list), /sessions delete <id>, /resume <id>, /model (not yet supported), /help. Anything else is sent to the agent.")
	default:
		c.send(s, channelID, "Unknown command. Try /help.")
	}
}

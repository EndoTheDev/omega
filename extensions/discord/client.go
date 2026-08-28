package discord

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// ensureSession returns the omega session id for a Discord channel,
// creating it if this is the first message on that channel.
func (c *Client) ensureSession(channelID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id, ok := c.byChannel[channelID]; ok {
		return id, nil
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.gatewayPost("/sessions", map[string]any{}, &created); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("gateway returned empty session id")
	}
	c.byChannel[channelID] = created.ID
	return created.ID, nil
}

// chat sends a user message to the gateway session and returns the reply.
func (c *Client) chat(sessionID, text string) (string, error) {
	body := map[string]any{
		"session_id": sessionID,
		"messages": []map[string]any{
			{"role": "user", "content": text},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, c.gateway+"/chat", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("gateway %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	return readSSEResponse(resp.Body)
}

// gatewayPost performs a JSON POST to the gateway and decodes the reply.
func (c *Client) gatewayPost(path string, payload map[string]any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.gateway+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("gateway %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// gatewayGet performs a JSON GET to the gateway and decodes the reply.
func (c *Client) gatewayGet(path string, out any) error {
	resp, err := c.http.Get(c.gateway + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("gateway %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// gatewayDelete performs a DELETE to the gateway.
func (c *Client) gatewayDelete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.gateway+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("gateway %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// health checks the gateway /health endpoint.
func (c *Client) health() (bool, error) {
	resp, err := c.http.Get(c.gateway + "/health")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// send posts a message to a Discord channel.
func (c *Client) send(s *discordgo.Session, channelID, text string) {
	const cap = 1900
	for len(text) > cap {
		if _, err := s.ChannelMessageSend(channelID, text[:cap]); err != nil {
			fmt.Printf("omega-discord: send: %v\n", err)
			return
		}
		text = text[cap:]
	}
	if _, err := s.ChannelMessageSend(channelID, text); err != nil {
		fmt.Printf("omega-discord: send: %v\n", err)
	}
}

// readSSEResponse parses the gateway's SSE stream and returns the joined
// text of every "response_chunk" event.
func readSSEResponse(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out strings.Builder
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var ev struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			if eventType == "response_chunk" {
				out.WriteString(ev.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return out.String(), err
	}
	return strings.TrimSpace(out.String()), nil
}

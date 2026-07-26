package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	webhook string
	http    *http.Client
}

func New(webhookURL string) *Client {
	return &Client{
		webhook: webhookURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c.webhook != ""
}

func (c *Client) Send(content string) error {
	if !c.Enabled() {
		return nil
	}
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	resp, err := c.http.Post(c.webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned %s", resp.Status)
	}
	return nil
}

func (c *Client) SendTest() error {
	return c.Send("pelican-steam-updater test notification — Discord webhook is working.")
}

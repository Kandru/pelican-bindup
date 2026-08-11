// Package pelican talks to the Pelican Client API (power, resources).
package pelican

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kandru/pelican-bindup/internal/util"
)

type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

type Resources struct {
	CurrentState string
	Uptime       int64 // milliseconds
}

func NewClient(panelURL, apiKey string) *Client {
	base := strings.TrimRight(panelURL, "/")
	if !strings.HasSuffix(base, "/api/client") {
		base += "/api/client"
	}
	return &Client{
		baseURL: base,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) GetResources(serverUUID string) (*Resources, error) {
	body, err := c.do(http.MethodGet, "/servers/"+serverUUID+"/resources", nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Attributes struct {
			CurrentState string `json:"current_state"`
			Resources    struct {
				Uptime int64 `json:"uptime"`
			} `json:"resources"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode resources: %w", err)
	}
	return &Resources{
		CurrentState: parsed.Attributes.CurrentState,
		Uptime:       parsed.Attributes.Resources.Uptime,
	}, nil
}

// ServerName returns the panel display name for a server.
func (c *Client) ServerName(serverUUID string) (string, error) {
	body, err := c.do(http.MethodGet, "/servers/"+serverUUID, nil)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Attributes struct {
			Name string `json:"name"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode server: %w", err)
	}
	if parsed.Attributes.Name == "" {
		return "", fmt.Errorf("empty server name")
	}
	return parsed.Attributes.Name, nil
}

func (c *Client) Power(serverUUID, signal string) error {
	_, err := c.do(http.MethodPost, "/servers/"+serverUUID+"/power", map[string]string{"signal": signal})
	return err
}

func (c *Client) WaitForState(serverUUID, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := c.GetResources(serverUUID)
		if err == nil && res.CurrentState == want {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for state %q", want)
}

func (c *Client) do(method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s %s: %s", method, path, util.Truncate(data, 200))
	}
	return data, nil
}

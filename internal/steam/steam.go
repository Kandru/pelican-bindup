package steam

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var acfBuildIDRe = regexp.MustCompile(`"buildid"\s+"(\d+)"`)

type Checker struct {
	infoAPI string
	client  *http.Client
}

type BuildInfo struct {
	Remote string
	Local  string
	Update bool
}

func NewChecker(infoAPI string) *Checker {
	return &Checker{
		infoAPI: strings.TrimRight(infoAPI, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Checker) Ping(appID int) (buildID string, err error) {
	return c.remoteBuildID(appID)
}

func (c *Checker) Check(appID int, mainVolume, manifestRelative string) (*BuildInfo, error) {
	remote, err := c.remoteBuildID(appID)
	if err != nil {
		return nil, err
	}
	local, err := c.localBuildID(mainVolume, manifestRelative)
	if err != nil {
		return nil, err
	}
	info := &BuildInfo{Remote: remote, Local: local}
	if remote == "" {
		return info, nil
	}
	if local == "" {
		info.Update = true
		return info, nil
	}
	ri, _ := strconv.ParseInt(remote, 10, 64)
	li, _ := strconv.ParseInt(local, 10, 64)
	info.Update = ri > li
	return info, nil
}

func (c *Checker) remoteBuildID(appID int) (string, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/%d", c.infoAPI, appID))
	if err != nil {
		return "", fmt.Errorf("steam info api: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("steam info api %s", resp.Status)
	}
	return parseRemoteBuildID(body, appID)
}

func parseRemoteBuildID(body []byte, appID int) (string, error) {
	var parsed struct {
		Data map[string]struct {
			Depots struct {
				Branches map[string]struct {
					BuildID string `json:"buildid"`
				} `json:"branches"`
			} `json:"depots"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse steam api response: %w", err)
	}
	app, ok := parsed.Data[strconv.Itoa(appID)]
	if !ok {
		return "", fmt.Errorf("app %d not in steam api response", appID)
	}
	if pub, ok := app.Depots.Branches["public"]; ok && pub.BuildID != "" {
		return pub.BuildID, nil
	}
	var latest string
	var latestID int64
	for _, branch := range app.Depots.Branches {
		if branch.BuildID == "" {
			continue
		}
		id, err := strconv.ParseInt(branch.BuildID, 10, 64)
		if err != nil {
			continue
		}
		if id > latestID {
			latestID = id
			latest = branch.BuildID
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no buildid found for app %d", appID)
	}
	return latest, nil
}

func (c *Checker) localBuildID(mainVolume, manifestRelative string) (string, error) {
	path := filepath.Join(mainVolume, manifestRelative)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read manifest %s: %w", path, err)
	}
	return extractACFBuildID(string(data)), nil
}

func extractACFBuildID(content string) string {
	if m := acfBuildIDRe.FindStringSubmatch(content); len(m) == 2 {
		return m[1]
	}
	return ""
}

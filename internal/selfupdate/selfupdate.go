// Package selfupdate replaces the binary from the latest GitHub release.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
)

type Updater struct {
	repo    string
	version string
	log     *ui.Logger
}

func New(repo, version string, log *ui.Logger) *Updater {
	return &Updater{repo: repo, version: version, log: log}
}

func (u *Updater) Run(installPath string) error {
	if u.repo == "" || strings.HasPrefix(u.repo, "OWNER/") {
		return fmt.Errorf("self_update.github_repo is not configured")
	}

	u.log.Step(ui.StatusStart, "check latest release for %s", u.repo)
	assetName := fmt.Sprintf("pelican-steam-updater_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+u.repo+"/releases/latest", nil)
	if err != nil {
		return fmt.Errorf("github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pelican-steam-updater")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return err
	}

	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("asset %q not found in release %s", assetName, release.TagName)
	}

	if strings.TrimPrefix(release.TagName, "v") == strings.TrimPrefix(u.version, "v") {
		u.log.Step(ui.StatusOK, "already at latest (%s)", release.TagName)
		return nil
	}

	u.log.Step(ui.StatusStart, "download %s from %s", assetName, release.TagName)
	tmpDir, err := os.MkdirTemp("", "pelican-steam-updater-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(client, downloadURL, archivePath); err != nil {
		return err
	}

	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return err
	}

	if installPath == "" {
		installPath, err = os.Executable()
		if err != nil {
			return err
		}
	}

	u.log.Step(ui.StatusStart, "install to %s", installPath)
	if err := installFile(binaryPath, installPath); err != nil {
		return err
	}
	u.log.Step(ui.StatusOK, "updated to %s", release.TagName)
	return nil
}

func downloadFile(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractBinary(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	out := filepath.Join(destDir, "pelican-steam-updater")
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg || !strings.Contains(hdr.Name, "pelican-steam-updater") {
			continue
		}
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(w, tr); err != nil {
			_ = w.Close()
			return "", err
		}
		_ = w.Close()
		return out, nil
	}
	return "", fmt.Errorf("binary not found in archive")
}

func installFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
)

const LatestReleaseURL = "https://api.github.com/repos/callumalpass/tickle/releases/latest"

type Options struct {
	CurrentVersion string
	GOOS           string
	GOARCH         string
	LatestURL      string
	HTTPClient     *http.Client
}

type Asset struct {
	Name string
	URL  string
}

type Result struct {
	CurrentVersion    string
	CurrentComparable bool
	LatestVersion     string
	LatestTag         string
	ReleaseURL        string
	Platform          string
	UpdateAvailable   bool
	BinaryAsset       *Asset
	SkillAsset        *Asset
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func Check(ctx context.Context, opts Options) (Result, error) {
	current := strings.TrimSpace(opts.CurrentVersion)
	if current == "" {
		current = "unknown"
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	latestURL := opts.LatestURL
	if latestURL == "" {
		latestURL = LatestReleaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	release, err := fetchLatestRelease(ctx, client, latestURL, current)
	if err != nil {
		return Result{}, err
	}

	latest := normalizeVersion(release.TagName)
	currentNormalized := normalizeVersion(current)
	updateAvailable, comparable := newerVersion(latest, currentNormalized)
	platform := goos + "-" + goarch
	binaryName := "tickle-" + platform
	if goos == "windows" {
		binaryName += ".exe"
	}
	skillName := "tickle-skill-" + platform + ".zip"

	result := Result{
		CurrentVersion:    current,
		CurrentComparable: comparable,
		LatestVersion:     latest,
		LatestTag:         release.TagName,
		ReleaseURL:        release.HTMLURL,
		Platform:          platform,
		UpdateAvailable:   updateAvailable,
	}
	for _, asset := range release.Assets {
		candidate := Asset{Name: asset.Name, URL: asset.BrowserDownloadURL}
		switch asset.Name {
		case binaryName:
			result.BinaryAsset = &candidate
		case skillName:
			result.SkillAsset = &candidate
		}
	}
	return result, nil
}

func fetchLatestRelease(ctx context.Context, client *http.Client, url, currentVersion string) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tickle/"+strings.TrimPrefix(currentVersion, "v"))

	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		detail := strings.TrimSpace(string(body))
		if detail != "" {
			return githubRelease{}, fmt.Errorf("update check failed: %s: %s", resp.Status, detail)
		}
		return githubRelease{}, fmt.Errorf("update check failed: %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if release.TagName == "" {
		return githubRelease{}, fmt.Errorf("update check failed: latest release did not include tag_name")
	}
	return release, nil
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if i := strings.IndexAny(version, "+-"); i >= 0 {
		version = version[:i]
	}
	return version
}

func newerVersion(latest, current string) (bool, bool) {
	latestParts, okLatest := parseVersion(latest)
	currentParts, okCurrent := parseVersion(current)
	if !okLatest || !okCurrent {
		return false, false
	}
	for i := range latestParts {
		if latestParts[i] > currentParts[i] {
			return true, true
		}
		if latestParts[i] < currentParts[i] {
			return false, true
		}
	}
	return false, true
}

func parseVersion(version string) ([3]int, bool) {
	var parsed [3]int
	parts := strings.Split(version, ".")
	if len(parts) == 0 || len(parts) > len(parsed) {
		return parsed, false
	}
	for i, part := range parts {
		if part == "" {
			return parsed, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return parsed, false
		}
		parsed[i] = value
	}
	return parsed, true
}

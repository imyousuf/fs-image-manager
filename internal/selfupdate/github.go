package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GitHubSource resolves releases via the GitHub REST API. BaseURL defaults to
// the public API; tests point it at an httptest server. It implements
// ReleaseSource.
type GitHubSource struct {
	Owner   string
	Repo    string
	BaseURL string // e.g. "https://api.github.com"; no trailing slash
	Client  *http.Client
}

// NewGitHubSource builds a GitHubSource for the public API with a sane timeout.
func NewGitHubSource(owner, repo string) *GitHubSource {
	return &GitHubSource{
		Owner:   owner,
		Repo:    repo,
		BaseURL: "https://api.github.com",
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// ghRelease mirrors the subset of the GitHub release JSON we consume.
type ghRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (g *GitHubSource) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return http.DefaultClient
}

// Latest returns the repository's latest non-draft, non-prerelease release.
func (g *GitHubSource) Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", g.BaseURL, g.Owner, g.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.client().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("query latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Release{}, fmt.Errorf("latest release: unexpected status %d: %s", resp.StatusCode, body)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("decode release JSON: %w", err)
	}
	if rel.TagName == "" {
		return Release{}, fmt.Errorf("selfupdate: latest release has no tag")
	}
	out := Release{Version: rel.TagName, Assets: make(map[string]string, len(rel.Assets))}
	for _, a := range rel.Assets {
		out.Assets[a.Name] = a.BrowserDownloadURL
	}
	return out, nil
}

// Fetch downloads the named asset from the release.
func (g *GitHubSource) Fetch(ctx context.Context, rel Release, assetName string) ([]byte, error) {
	url, ok := rel.Assets[assetName]
	if !ok {
		return nil, fmt.Errorf("selfupdate: release %s has no asset %q", rel.Version, assetName)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := g.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download asset %q: %w", assetName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download asset %q: unexpected status %d: %s", assetName, resp.StatusCode, body)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBinarySize))
	if err != nil {
		return nil, fmt.Errorf("read asset %q: %w", assetName, err)
	}
	return data, nil
}

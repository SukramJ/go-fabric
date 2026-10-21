// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// GitHubConfig names the development-certificate repository — matter.js
// DclGithubConfig (DclConfig.ts:34-53).
type GitHubConfig struct {
	// APIURL is the GitHub API; empty is https://api.github.com.
	APIURL string
	// Owner, Repo and Branch name the repository.
	Owner, Repo, Branch string
	// CertPath is the directory of the PAA .der files, CDSignerCertPath
	// that of the CD signers.
	CertPath, CDSignerCertPath string
	// Timeout bounds each request; 0 is none, as matter.js's GitHub client
	// without a timeout option (packages/general/src/util/Github.ts:131-133).
	Timeout time.Duration
}

// DefaultGitHubConfig is DclGithubConfig.defaults (DclConfig.ts:46-52):
// connectedhomeip's development certificates on master.
var DefaultGitHubConfig = GitHubConfig{
	APIURL: "https://api.github.com",
	Owner:  "project-chip", Repo: "connectedhomeip", Branch: "master",
	CertPath:         "credentials/development/paa-root-certs",
	CDSignerCertPath: "credentials/development/cd-certs",
}

// githubClient is matter.js's minimal GitHub client
// (packages/general/src/util/Github.ts:19-159) over the git trees API.
type githubClient struct {
	cfg  GitHubConfig
	http *http.Client
}

func newGitHubClient(cfg GitHubConfig, hc *http.Client) *githubClient {
	d := DefaultGitHubConfig
	if cfg.APIURL == "" {
		cfg.APIURL = d.APIURL
	}
	if cfg.Owner == "" {
		cfg.Owner, cfg.Repo, cfg.Branch = d.Owner, d.Repo, d.Branch
	}
	if cfg.CertPath == "" {
		cfg.CertPath = d.CertPath
	}
	if cfg.CDSignerCertPath == "" {
		cfg.CDSignerCertPath = d.CDSignerCertPath
	}
	return &githubClient{cfg: cfg, http: hc}
}

// gitHubHTTPError is Github.HttpError (Github.ts:29-41).
type gitHubHTTPError struct {
	msg    string
	status int
}

func (e *gitHubHTTPError) Error() string { return e.msg }

// isRateLimit: the statuses GitHub signals rate limiting with.
func (e *gitHubHTTPError) isRateLimit() bool { return e.status == 403 || e.status == 429 }

// repo is Github.Repo (Github.ts:147-158): the tree of the branch.
func (g *githubClient) repo() *githubDir {
	return &githubDir{g: g, url: fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", strings.TrimRight(g.cfg.APIURL, "/"), g.cfg.Owner, g.cfg.Repo, g.cfg.Branch)}
}

type githubEntry struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

// githubDir is Github.Directory (Github.ts:43-142).
type githubDir struct {
	g   *githubClient
	url string

	// mu guards contents.
	mu       sync.Mutex
	contents map[string]githubEntry
}

func (d *githubDir) ls(ctx context.Context) ([]string, error) {
	if err := d.load(ctx); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	names := make([]string, 0, len(d.contents))
	for n := range d.contents {
		names = append(names, n)
	}
	return names, nil
}

func (d *githubDir) cd(ctx context.Context, name string) (*githubDir, error) {
	result := d
	for _, p := range strings.Split(name, "/") {
		e, err := result.find(ctx, p)
		if err != nil {
			return nil, err
		}
		if e.Type != "tree" {
			return nil, fmt.Errorf("path %q not a directory", p)
		}
		result = &githubDir{g: d.g, url: e.URL}
	}
	return result, nil
}

func (d *githubDir) getBinary(ctx context.Context, name string) ([]byte, error) {
	e, err := d.find(ctx, name)
	if err != nil {
		return nil, err
	}
	if e.Type != "blob" {
		return nil, fmt.Errorf("path %q not a regular file", name)
	}
	return d.fetch(ctx, e.URL)
}

func (d *githubDir) load(ctx context.Context) error {
	d.mu.Lock()
	loaded := len(d.contents) > 0
	d.mu.Unlock()
	if loaded {
		return nil
	}
	body, err := d.fetch(ctx, d.url)
	if err != nil {
		return err
	}
	var resp struct {
		Tree []githubEntry `json:"tree"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}
	if resp.Tree == nil {
		return errors.New("dcl: no tree in fetch response")
	}
	d.mu.Lock()
	d.contents = make(map[string]githubEntry, len(resp.Tree))
	for _, e := range resp.Tree {
		d.contents[e.Path] = e
	}
	d.mu.Unlock()
	return nil
}

func (d *githubDir) find(ctx context.Context, name string) (githubEntry, error) {
	if err := d.load(ctx); err != nil {
		return githubEntry{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.contents[name]
	if !ok {
		return githubEntry{}, fmt.Errorf("path %q not found", name)
	}
	return e, nil
}

// fetch is Directory.doFetch (Github.ts:120-141): Accept
// application/vnd.github.raw, any status but 200 an HTTP error.
func (d *githubDir) fetch(ctx context.Context, url string) ([]byte, error) {
	if d.g.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.g.cfg.Timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw")
	resp, err := d.g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &gitHubHTTPError{msg: fmt.Sprintf("HTTP error %s (%d) from %s", http.StatusText(resp.StatusCode), resp.StatusCode, url), status: resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}

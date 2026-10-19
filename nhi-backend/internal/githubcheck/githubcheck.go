// Package githubcheck tells whether a GitHub token can do what the secret
// scanner needs: see repositories and read their contents. The token is used
// for these calls only; it is never logged or stored.
package githubcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrTokenRejected: GitHub does not accept the token (wrong, expired or revoked).
	ErrTokenRejected = errors.New("github rejected the token")
	// ErrNoRepositories: the token works but can see no repository.
	ErrNoRepositories = errors.New("token can see no repositories")
	// ErrNoContents: the token can list repositories but not read files.
	ErrNoContents = errors.New("token cannot read repository contents")
	// ErrUnavailable: GitHub could not be reached or is rate limiting.
	ErrUnavailable = errors.New("github is unavailable")
)

// Result describes what the token can reach.
type Result struct {
	Login string
	// Repositories seen on the first page (up to 100); More is true when
	// there are further pages.
	Repositories int
	More         bool
}

type Checker struct {
	baseURL string
	client  *http.Client
}

// New returns a checker for the GitHub API at baseURL (https://api.github.com,
// or https://<host>/api/v3 for GitHub Enterprise Server).
func New(baseURL string) *Checker {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &Checker{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 8 * time.Second,
			// Never forward the token to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c *Checker) get(ctx context.Context, token, path string, dst any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "nhi-connection-check")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if dst != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, dst); err != nil {
			return resp, fmt.Errorf("%w: unexpected response from GitHub", ErrUnavailable)
		}
	}
	return resp, nil
}

// Check validates the token.
func (c *Checker) Check(ctx context.Context, token string) (Result, error) {
	var res Result
	token = strings.TrimSpace(token)
	if token == "" {
		return res, ErrTokenRejected
	}
	var user struct {
		Login string `json:"login"`
	}
	resp, err := c.get(ctx, token, "/user", &user)
	if err != nil {
		return res, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return res, ErrTokenRejected
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return res, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return res, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	res.Login = user.Login

	var repos []struct {
		FullName string `json:"full_name"`
	}
	resp, err = c.get(ctx, token, "/user/repos?per_page=100&sort=pushed", &repos)
	if err != nil {
		return res, err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return res, ErrTokenRejected
		}
		return res, fmt.Errorf("%w: status %d listing repositories", ErrUnavailable, resp.StatusCode)
	}
	res.Repositories = len(repos)
	res.More = strings.Contains(resp.Header.Get("Link"), `rel="next"`)
	if res.Repositories == 0 {
		return res, ErrNoRepositories
	}

	// Reading the root of the most recently pushed repository proves the
	// token has Contents read access, not only Metadata.
	parts := strings.SplitN(repos[0].FullName, "/", 2)
	if len(parts) != 2 {
		return res, nil
	}
	resp, err = c.get(ctx, token, "/repos/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1])+"/contents/", nil)
	if err != nil {
		return res, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return res, nil
	case http.StatusNotFound:
		// An empty repository has no contents; nothing to disprove.
		return res, nil
	case http.StatusForbidden:
		return res, ErrNoContents
	default:
		return res, fmt.Errorf("%w: status %d reading contents", ErrUnavailable, resp.StatusCode)
	}
}

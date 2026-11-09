// Package mattermost is a minimal client for the parts of the Mattermost REST API v4
// and WebSocket API that the bridge uses.
package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	ServerURL string
	Token     string
	HTTP      *http.Client
	UserAgent string

	SessionCheckInterval time.Duration
}

type APIError struct {
	StatusCode int    `json:"status_code"`
	ID         string `json:"id"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s (%s)", e.StatusCode, e.Message, e.ID)
}

// NormalizeServerURL turns user input like "chat.example.org/" into "https://chat.example.org".
func NormalizeServerURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return "", err
	} else if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	} else if u.Host == "" {
		return "", fmt.Errorf("missing host in server URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

// Login creates a session with a username or email and a password and stores its token in c.
func (c *Client) Login(ctx context.Context, loginID, password string) (*User, error) {
	body := map[string]string{"login_id": loginID, "password": password}
	resp, err := c.do(ctx, http.MethodPost, "/users/login", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.Token = resp.Header.Get("Token")
	if c.Token == "" {
		return nil, fmt.Errorf("login response didn't include a session token")
	}
	var user User
	return &user, json.NewDecoder(resp.Body).Decode(&user)
}

func (c *Client) Logout(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, "/users/logout", nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// Ping checks that ServerURL points to a Mattermost server. It needs no session.
func (c *Client) Ping(ctx context.Context) error {
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.getJSON(ctx, "/system/ping", &resp); err != nil {
		return err
	} else if resp.Status != "OK" {
		return fmt.Errorf("unexpected ping status %q", resp.Status)
	}
	return nil
}

func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var user User
	return &user, c.getJSON(ctx, "/users/me", &user)
}

func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	var user User
	return &user, c.getJSON(ctx, "/users/"+url.PathEscape(userID), &user)
}

func (c *Client) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	return &user, c.getJSON(ctx, "/users/username/"+url.PathEscape(username), &user)
}

func (c *Client) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	return &user, c.getJSON(ctx, "/users/email/"+url.PathEscape(email), &user)
}

func (c *Client) GetChannel(ctx context.Context, channelID string) (*Channel, error) {
	var channel Channel
	return &channel, c.getJSON(ctx, "/channels/"+url.PathEscape(channelID), &channel)
}

// channelMembersPerPage is the most Mattermost returns per request.
const channelMembersPerPage = 200

// GetChannelMembers returns every member of a channel, following the pagination.
func (c *Client) GetChannelMembers(ctx context.Context, channelID string) ([]ChannelMember, error) {
	var members []ChannelMember
	for page := 0; ; page++ {
		var batch []ChannelMember
		path := fmt.Sprintf("/channels/%s/members?page=%d&per_page=%d", url.PathEscape(channelID), page, channelMembersPerPage)
		if err := c.getJSON(ctx, path, &batch); err != nil {
			return nil, err
		}
		members = append(members, batch...)
		if len(batch) < channelMembersPerPage {
			return members, nil
		}
	}
}

// CreateDirectChannel returns the direct message channel between two users, creating it if there is none yet.
func (c *Client) CreateDirectChannel(ctx context.Context, userID, otherUserID string) (*Channel, error) {
	return c.postChannel(ctx, "/channels/direct", []string{userID, otherUserID})
}

// CreateGroupChannel returns the group message channel with exactly these members, creating it if
// there is none yet. Mattermost wants 3 to 8 users, the caller included.
func (c *Client) CreateGroupChannel(ctx context.Context, userIDs []string) (*Channel, error) {
	return c.postChannel(ctx, "/channels/group", userIDs)
}

func (c *Client) postChannel(ctx context.Context, path string, userIDs []string) (*Channel, error) {
	resp, err := c.do(ctx, http.MethodPost, path, userIDs)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var channel Channel
	return &channel, json.NewDecoder(resp.Body).Decode(&channel)
}

// CreatePost sends a post and returns it as the server stored it.
func (c *Client) CreatePost(ctx context.Context, post *Post) (*Post, error) {
	resp, err := c.do(ctx, http.MethodPost, "/posts", post)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var created Post
	return &created, json.NewDecoder(resp.Body).Decode(&created)
}

func (c *Client) GetProfileImage(ctx context.Context, userID string) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(userID)+"/image", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(into)
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.ServerURL+"/api/v4"+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(req.Header)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		apiErr := &APIError{}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(apiErr)
		apiErr.StatusCode = resp.StatusCode
		return nil, apiErr
	}
	return resp, nil
}

func (c *Client) setHeaders(h http.Header) {
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
	if c.UserAgent != "" {
		h.Set("User-Agent", c.UserAgent)
	}
}

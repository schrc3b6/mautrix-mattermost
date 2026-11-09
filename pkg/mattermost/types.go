package mattermost

import (
	"encoding/json"
	"strings"
)

type User struct {
	ID                string `json:"id"`
	Username          string `json:"username"`
	Email             string `json:"email"`
	Nickname          string `json:"nickname"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	LastPictureUpdate int64  `json:"last_picture_update"`
	IsBot             bool   `json:"is_bot"`
}

// DisplayName follows Mattermost's default "nickname, else full name, else username" order.
func (u *User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	if fullName := strings.TrimSpace(u.FirstName + " " + u.LastName); fullName != "" {
		return fullName
	}
	return u.Username
}

type ChannelType string

const (
	ChannelOpen    ChannelType = "O"
	ChannelPrivate ChannelType = "P"
	ChannelDirect  ChannelType = "D"
	ChannelGroup   ChannelType = "G"
)

type Channel struct {
	ID          string      `json:"id"`
	TeamID      string      `json:"team_id"`
	Type        ChannelType `json:"type"`
	DisplayName string      `json:"display_name"`
	Name        string      `json:"name"`
	Header      string      `json:"header"`
	Purpose     string      `json:"purpose"`
}

// IsDirectOrGroup reports whether the channel is a direct or group message rather than a team channel.
func (t ChannelType) IsDirectOrGroup() bool {
	return t == ChannelDirect || t == ChannelGroup
}

// OtherDirectMessageUserID returns the other member of a direct message channel. Mattermost names
// these "<user id>__<user id>" with the IDs sorted, so no request is needed. A DM with yourself
// has your ID on both sides.
func (ch *Channel) OtherDirectMessageUserID(ownUserID string) (string, bool) {
	if ch.Type != ChannelDirect {
		return "", false
	}
	a, b, ok := strings.Cut(ch.Name, "__")
	if !ok || a == "" || b == "" {
		return "", false
	} else if a == ownUserID {
		return b, true
	} else if b == ownUserID {
		return a, true
	}
	return "", false
}

type ChannelMember struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
}

type Post struct {
	ID        string   `json:"id,omitempty"`
	CreateAt  int64    `json:"create_at,omitempty"`
	UserID    string   `json:"user_id,omitempty"`
	ChannelID string   `json:"channel_id"`
	RootID    string   `json:"root_id,omitempty"`
	Message   string   `json:"message"`
	Type      string   `json:"type,omitempty"`
	FileIDs   []string `json:"file_ids,omitempty"`
	// PendingPostID is set by the sending client and comes back unchanged in the posted event.
	PendingPostID string `json:"pending_post_id,omitempty"`
}

// IsSystem reports whether the post is a system message like "X joined the channel".
func (p *Post) IsSystem() bool {
	return p.Type != ""
}

const EventPosted = "posted"

type WebSocketEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
	Seq   int64           `json:"seq"`
}

type PostedData struct {
	ChannelType        ChannelType `json:"channel_type"`
	ChannelDisplayName string      `json:"channel_display_name"`
	SenderName         string      `json:"sender_name"`
	// Post is a JSON-encoded Post inside the JSON event.
	Post string `json:"post"`
}

func (d *PostedData) ParsePost() (*Post, error) {
	var post Post
	return &post, json.Unmarshal([]byte(d.Post), &post)
}

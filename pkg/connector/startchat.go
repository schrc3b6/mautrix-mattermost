package connector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

var (
	_ bridgev2.IdentifierResolvingNetworkAPI = (*MattermostClient)(nil)
	_ bridgev2.GroupCreatingNetworkAPI       = (*MattermostClient)(nil)
)

// Mattermost IDs are 26 characters of base32-ish lowercase letters and digits.
var mattermostIDRegex = regexp.MustCompile(`^[a-z0-9]{26}$`)

type identifierKind int

const (
	identifierUsername identifierKind = iota
	identifierEmail
	identifierUserID
)

// parseIdentifier accepts "alice", "@alice", "alice@example.org" or a raw user ID.
func parseIdentifier(identifier string) (identifierKind, string) {
	identifier = strings.TrimSpace(identifier)
	if strings.HasPrefix(identifier, "@") {
		return identifierUsername, strings.TrimPrefix(identifier, "@")
	} else if strings.Contains(identifier, "@") {
		return identifierEmail, identifier
	} else if mattermostIDRegex.MatchString(identifier) {
		return identifierUserID, identifier
	}
	return identifierUsername, identifier
}

// ResolveIdentifier finds a Mattermost user by username, email or ID and, if asked,
// the DM channel with them.
func (mc *MattermostClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	var user *mattermost.User
	var err error
	switch kind, value := parseIdentifier(identifier); kind {
	case identifierEmail:
		user, err = mc.Client.GetUserByEmail(ctx, value)
	case identifierUserID:
		user, err = mc.Client.GetUser(ctx, value)
	default:
		user, err = mc.Client.GetUserByUsername(ctx, value)
	}
	var apiErr *mattermost.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil, bridgev2.RespError{
			ErrCode:    mautrix.MNotFound.ErrCode,
			Err:        fmt.Sprintf("No Mattermost user %q", identifier),
			StatusCode: http.StatusNotFound,
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to look up user: %w", err)
	}
	ghost, err := mc.Main.Bridge.GetGhostByID(ctx, networkUserID(user.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to get ghost: %w", err)
	}
	resp := &bridgev2.ResolveIdentifierResponse{
		Ghost:    ghost,
		UserID:   networkUserID(user.ID),
		UserInfo: mc.userInfo(user),
	}
	if createChat {
		channel, err := mc.Client.CreateDirectChannel(ctx, mc.ownUserID, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to create direct channel: %w", err)
		}
		resp.Chat = &bridgev2.CreateChatResponse{
			PortalKey: mc.makePortalKey(channel.ID, mattermost.ChannelDirect),
		}
	}
	return resp, nil
}

// CreateGroup starts a group message with the participants. Mattermost names it after the
// members and has no topic or avatar for group messages, so those params are ignored.
func (mc *MattermostClient) CreateGroup(ctx context.Context, params *bridgev2.GroupCreateParams) (*bridgev2.CreateChatResponse, error) {
	userIDs := make([]string, 0, len(params.Participants)+1)
	userIDs = append(userIDs, mc.ownUserID)
	for _, participant := range params.Participants {
		if string(participant) != mc.ownUserID {
			userIDs = append(userIDs, string(participant))
		}
	}
	// Below three is a DM, above eight Mattermost refuses with a less helpful message.
	if len(userIDs) < 3 || len(userIDs) > 8 {
		return nil, bridgev2.RespError{
			ErrCode:    mautrix.MInvalidParam.ErrCode,
			Err:        "A Mattermost group message has 3 to 8 members, you included",
			StatusCode: http.StatusBadRequest,
		}
	}
	channel, err := mc.Client.CreateGroupChannel(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create group channel: %w", err)
	}
	return &bridgev2.CreateChatResponse{
		PortalKey: mc.makePortalKey(channel.ID, mattermost.ChannelGroup),
	}, nil
}

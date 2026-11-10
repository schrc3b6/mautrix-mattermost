package connector

import (
	"context"
	"fmt"
	"strconv"

	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

func (mc *MattermostClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	channel, err := mc.Client.GetChannel(ctx, string(portal.ID))
	if err != nil {
		return nil, err
	}
	switch channel.Type {
	case mattermost.ChannelDirect:
		return mc.directChatInfo(ctx, channel)
	case mattermost.ChannelGroup:
		return mc.groupChatInfo(ctx, channel)
	}
	topic := channel.Purpose
	if topic == "" {
		topic = channel.Header
	}
	return &bridgev2.ChatInfo{
		Name:  &channel.DisplayName,
		Topic: &topic,
		// Other members join as they post. Syncing the full member list comes later.
		Members: mc.memberList(false, mc.ownUserID),
		Type:    ptr.Ptr(database.RoomTypeDefault),
	}, nil
}

// directChatInfo describes a DM. No name or topic: Matrix clients name a two-member room after
// the other member, which follows their profile, and Mattermost DMs have no name of their own.
func (mc *MattermostClient) directChatInfo(ctx context.Context, channel *mattermost.Channel) (*bridgev2.ChatInfo, error) {
	otherUserID, ok := channel.OtherDirectMessageUserID(mc.ownUserID)
	if !ok {
		// The name should always parse, but the member list is the authority.
		members, err := mc.Client.GetChannelMembers(ctx, channel.ID)
		if err != nil {
			return nil, err
		}
		otherUserID = mc.ownUserID
		for _, member := range members {
			if member.UserID != mc.ownUserID {
				otherUserID = member.UserID
			}
		}
	}
	info := &bridgev2.ChatInfo{
		Members: mc.memberList(true, mc.ownUserID, otherUserID),
		Type:    ptr.Ptr(database.RoomTypeDM),
	}
	info.Members.OtherUserID = networkid.UserID(otherUserID)
	return info, nil
}

// groupChatInfo describes a group message. Mattermost names them after the members
// ("alice, bob, carol") and keeps that up to date, so the display name is used as is.
func (mc *MattermostClient) groupChatInfo(ctx context.Context, channel *mattermost.Channel) (*bridgev2.ChatInfo, error) {
	members, err := mc.Client.GetChannelMembers(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, len(members))
	for i, member := range members {
		userIDs[i] = member.UserID
	}
	return &bridgev2.ChatInfo{
		Name:    &channel.DisplayName,
		Members: mc.memberList(true, userIDs...),
		Type:    ptr.Ptr(database.RoomTypeGroupDM),
	}, nil
}

func (mc *MattermostClient) memberList(full bool, userIDs ...string) *bridgev2.ChatMemberList {
	memberMap := make(map[networkid.UserID]bridgev2.ChatMember, len(userIDs))
	for _, userID := range userIDs {
		memberMap[networkid.UserID(userID)] = bridgev2.ChatMember{
			EventSender: mc.makeEventSender(userID),
			Membership:  event.MembershipJoin,
		}
	}
	return &bridgev2.ChatMemberList{IsFull: full, MemberMap: memberMap}
}

func (mc *MattermostClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	user, err := mc.Client.GetUser(ctx, string(ghost.ID))
	if err != nil {
		return nil, err
	}
	return mc.userInfo(user), nil
}

func (mc *MattermostClient) userInfo(user *mattermost.User) *bridgev2.UserInfo {
	return &bridgev2.UserInfo{
		Identifiers: []string{"mattermost:" + user.Username},
		Name:        ptr.Ptr(user.DisplayName()),
		IsBot:       &user.IsBot,
		Avatar: &bridgev2.Avatar{
			ID: networkid.AvatarID(strconv.FormatInt(user.LastPictureUpdate, 10)),
			Get: func(ctx context.Context) ([]byte, error) {
				return mc.Client.GetProfileImage(ctx, user.ID)
			},
		},
	}
}

func (mc *MattermostConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 1
}

func (mc *MattermostConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

func (mc *MattermostClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	// Mattermost takes any file type; the only limit is the server's size cap. Voice messages
	// and stickers go through as plain audio and image attachments.
	files := &event.FileFeatures{
		MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported},
		Caption:   event.CapLevelFullySupported,
		MaxSize:   mc.mattermostMaxFileSize(),
	}
	return &event.RoomFeatures{
		// The size limit is per server, so it's part of the ID.
		ID: fmt.Sprintf("com.github.robinchoice.mattermost.capabilities.2026_11_10.1+%d", files.MaxSize),
		Formatting: event.FormattingFeatureMap{
			event.FmtBold:               event.CapLevelFullySupported,
			event.FmtItalic:             event.CapLevelFullySupported,
			event.FmtUnderline:          event.CapLevelDropped,
			event.FmtStrikethrough:      event.CapLevelFullySupported,
			event.FmtInlineCode:         event.CapLevelFullySupported,
			event.FmtCodeBlock:          event.CapLevelFullySupported,
			event.FmtSyntaxHighlighting: event.CapLevelFullySupported,
			event.FmtBlockquote:         event.CapLevelFullySupported,
			event.FmtInlineLink:         event.CapLevelFullySupported,
			event.FmtUserLink:           event.CapLevelFullySupported,
			event.FmtUnorderedList:      event.CapLevelFullySupported,
			event.FmtOrderedList:        event.CapLevelFullySupported,
			event.FmtHeaders:            event.CapLevelFullySupported,
			event.FmtHorizontalLine:     event.CapLevelFullySupported,
		},
		File: event.FileFeatureMap{
			event.MsgImage:      files,
			event.MsgVideo:      files,
			event.MsgAudio:      files,
			event.MsgFile:       files,
			event.CapMsgVoice:   files,
			event.CapMsgGIF:     files,
			event.CapMsgSticker: files,
		},
		LocationMessage: event.CapLevelRejected,
		Thread:          event.CapLevelFullySupported,
		// Mattermost has no replies outside threads, bridgev2 turns Matrix replies into thread replies.
		Reply:    event.CapLevelUnsupported,
		Edit:     event.CapLevelRejected,
		Delete:   event.CapLevelRejected,
		Reaction: event.CapLevelRejected,
	}
}

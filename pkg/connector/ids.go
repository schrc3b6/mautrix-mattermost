package connector

import (
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

type UserLoginMetadata struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

func (mc *MattermostConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		UserLogin: func() any {
			return &UserLoginMetadata{}
		},
	}
}

func networkUserID(userID string) networkid.UserID {
	return networkid.UserID(userID)
}

func (mc *MattermostClient) makeEventSender(userID string) bridgev2.EventSender {
	return bridgev2.EventSender{
		IsFromMe:    userID == mc.ownUserID,
		SenderLogin: makeUserLoginID(mc.Client.ServerURL, userID),
		Sender:      networkUserID(userID),
	}
}

// makePortalKey keys team channels by channel ID alone, so Matrix users logged in to the same
// server share one room. Direct and group messages get the login as receiver: they are between
// this user and a few others, and two logins that are both in one would otherwise share a room
// in which "the other user" of a DM differs per login.
func (mc *MattermostClient) makePortalKey(channelID string, channelType mattermost.ChannelType) networkid.PortalKey {
	pk := networkid.PortalKey{ID: networkid.PortalID(channelID)}
	if mc.UserLogin.Bridge.Config.SplitPortals || channelType.IsDirectOrGroup() {
		pk.Receiver = mc.UserLogin.ID
	}
	return pk
}

// makeUserLoginID puts the server in front of the user ID, so logins on two servers can't collide.
func makeUserLoginID(serverURL, userID string) networkid.UserLoginID {
	_, host, _ := strings.Cut(serverURL, "://")
	return networkid.UserLoginID(host + "/" + userID)
}

// parseUserLoginID returns the Mattermost user ID of a login. Logins created before
// the server was part of the ID are just the user ID.
func parseUserLoginID(loginID networkid.UserLoginID) string {
	id := string(loginID)
	return id[strings.LastIndexByte(id, '/')+1:]
}

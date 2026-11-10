package connector

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

// Mattermost markdown has no underline and shows HTML tags literally.
var matrixHTMLParser = func() *format.HTMLParser {
	parser := *format.MarkdownHTMLParser
	parser.UnderlineConverter = func(s string, _ format.Context) string {
		return s
	}
	return &parser
}()

func (mc *MattermostClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	var post *mattermost.Post
	var err error
	if msg.Content.MsgType.IsMedia() {
		post, err = mc.convertMatrixMedia(ctx, msg)
	} else {
		post, err = convertMatrixMessage(ctx, msg, mc.getMattermostUsername)
	}
	if err != nil {
		return nil, err
	}
	// The posted event for our own post can arrive before the HTTP response.
	// Mattermost echoes pending_post_id, so the bridge can drop it until the post is saved.
	txnID := networkid.TransactionID(msg.Event.ID)
	post.PendingPostID = string(txnID)
	msg.AddPendingToIgnore(txnID)
	created, err := mc.Client.CreatePost(ctx, post)
	if err != nil {
		msg.RemovePending(txnID)
		return nil, bridgev2.WrapErrorInStatus(err).WithErrorAsMessage().WithSendNotice(true)
	}
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        networkid.MessageID(created.ID),
			SenderID:  networkid.UserID(created.UserID),
			Timestamp: time.UnixMilli(created.CreateAt),
		},
		StreamOrder:   created.CreateAt,
		RemovePending: txnID,
	}, nil
}

// getMattermostUsername finds the Mattermost username for a ghost or for a Matrix user logged in
// to the same server. It returns "" if there is none.
func (mc *MattermostClient) getMattermostUsername(ctx context.Context, mxid id.UserID) string {
	userID, isGhost := mc.Main.Bridge.Matrix.ParseGhostMXID(mxid)
	if !isGhost {
		user, err := mc.Main.Bridge.GetExistingUserByMXID(ctx, mxid)
		if err != nil || user == nil {
			return ""
		}
		for _, login := range user.GetUserLogins() {
			if login.Metadata.(*UserLoginMetadata).ServerURL == mc.Client.ServerURL {
				userID = networkid.UserID(parseUserLoginID(login.ID))
				break
			}
		}
		if userID == "" {
			return ""
		}
	}
	user, err := mc.Client.GetUser(ctx, string(userID))
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Stringer("mxid", mxid).Msg("Failed to get Mattermost user for mention")
		return ""
	}
	return user.Username
}

// usernameLookup finds the Mattermost username behind a Matrix user ID, "" if there is none.
type usernameLookup func(context.Context, id.UserID) string

func convertMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage, getUsername usernameLookup) (*mattermost.Post, error) {
	content := msg.Content
	if content.MsgType != event.MsgText && content.MsgType != event.MsgNotice {
		return nil, bridgev2.ErrUnsupportedMessageType
	}
	formatted := ""
	if content.Format == event.FormatHTML {
		formatted = content.FormattedBody
	}
	return newPost(msg, matrixToMarkdown(ctx, content.Body, formatted, getUsername)), nil
}

// matrixToMarkdown turns a message body into Mattermost markdown. The formatted body wins when
// there is one; mentions of Mattermost users become @username.
func matrixToMarkdown(ctx context.Context, body, formattedBody string, getUsername usernameLookup) string {
	if formattedBody == "" {
		return body
	}
	parser := *matrixHTMLParser
	parser.PillConverter = func(displayname, mxid, eventID string, fctx format.Context) string {
		if strings.HasPrefix(mxid, "@") {
			if username := getUsername(fctx.Ctx, id.UserID(mxid)); username != "" {
				return "@" + username
			}
		}
		return format.DefaultPillConverter(displayname, mxid, eventID, fctx)
	}
	return parser.Parse(formattedBody, format.NewContext(ctx))
}

// newPost makes a post for the message's channel, in the thread the message belongs to.
func newPost(msg *bridgev2.MatrixMessage, text string) *mattermost.Post {
	post := &mattermost.Post{ChannelID: string(msg.Portal.ID), Message: text}
	// Mattermost threads hang off the root post, replies to replies aren't a thing.
	if root := msg.ThreadRoot; root != nil {
		post.RootID = string(root.ID)
		if root.ThreadRoot != "" {
			post.RootID = string(root.ThreadRoot)
		}
	}
	return post
}

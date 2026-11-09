package connector

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

func (mc *MattermostClient) handleEvent(ctx context.Context, evt *mattermost.WebSocketEvent) {
	log := zerolog.Ctx(ctx)
	switch evt.Event {
	case mattermost.EventPosted:
		var data mattermost.PostedData
		if err := json.Unmarshal(evt.Data, &data); err != nil {
			log.Err(err).Int64("seq", evt.Seq).Msg("Failed to parse posted event")
			return
		}
		post, err := data.ParsePost()
		if err != nil {
			log.Err(err).Int64("seq", evt.Seq).Msg("Failed to parse post in posted event")
			return
		}
		mc.handlePost(ctx, data.ChannelType, post)
	default:
		log.Trace().Str("event_type", evt.Event).Msg("Ignoring unsupported event")
	}
}

func (mc *MattermostClient) handlePost(ctx context.Context, channelType mattermost.ChannelType, post *mattermost.Post) {
	log := zerolog.Ctx(ctx).With().Str("post_id", post.ID).Str("channel_id", post.ChannelID).Logger()
	if post.IsSystem() {
		log.Debug().Str("post_type", post.Type).Msg("Ignoring system post")
		return
	}
	mc.UserLogin.QueueRemoteEvent(&simplevent.Message[*mattermost.Post]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessage,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.ID).Str("channel_id", post.ChannelID)
			},
			PortalKey:    mc.makePortalKey(post.ChannelID, channelType),
			Sender:       mc.makeEventSender(post.UserID),
			CreatePortal: true,
			Timestamp:    time.UnixMilli(post.CreateAt),
			StreamOrder:  post.CreateAt,
		},
		Data:               post,
		TransactionID:      networkid.TransactionID(post.PendingPostID),
		ID:                 networkid.MessageID(post.ID),
		ConvertMessageFunc: convertPost,
	})
}

func convertPost(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, post *mattermost.Post) (*bridgev2.ConvertedMessage, error) {
	content := format.RenderMarkdown(post.Message, true, false)
	if len(post.FileIDs) > 0 {
		// TODO bridge attachments
		notice := "(attachment not bridged yet)"
		if content.Body == "" {
			content = event.MessageEventContent{MsgType: event.MsgNotice, Body: notice}
		} else {
			content.Body += "\n\n" + notice
			if content.FormattedBody != "" {
				content.FormattedBody += "<p><em>" + notice + "</em></p>"
			}
		}
	}
	converted := &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type:    event.EventMessage,
			Content: &content,
		}},
	}
	if post.RootID != "" {
		converted.ThreadRoot = (*networkid.MessageID)(&post.RootID)
	}
	return converted, nil
}

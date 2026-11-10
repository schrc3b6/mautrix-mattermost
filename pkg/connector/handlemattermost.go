package connector

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"

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
		ConvertMessageFunc: mc.convertPost,
	})
}

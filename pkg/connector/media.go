package connector

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

// defaultMatrixMaxFileSize is what bridgev2 says to assume until SetMaxFileSize is called.
const defaultMatrixMaxFileSize = 50 << 20

// Mattermost → Matrix

// convertPost turns a post into its Matrix parts: the text, if any, then one media part per
// attachment. An attachment that can't be bridged becomes a notice instead, so the rest of the
// post still gets through.
func (mc *MattermostClient) convertPost(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, post *mattermost.Post) (*bridgev2.ConvertedMessage, error) {
	converted := convertPostText(post)
	for _, file := range post.Files() {
		converted.Parts = append(converted.Parts, mc.convertAttachment(ctx, portal, intent, file))
	}
	if len(converted.Parts) == 0 {
		return nil, fmt.Errorf("post has neither text nor attachments")
	}
	return converted, nil
}

// convertPostText renders the post's markdown. A post with no text, which is what a
// file-only post is, gets no text part.
func convertPostText(post *mattermost.Post) *bridgev2.ConvertedMessage {
	converted := &bridgev2.ConvertedMessage{}
	if post.Message != "" {
		content := format.RenderMarkdown(post.Message, true, false)
		converted.Parts = append(converted.Parts, &bridgev2.ConvertedMessagePart{
			Type:    event.EventMessage,
			Content: &content,
		})
	}
	if post.RootID != "" {
		converted.ThreadRoot = (*networkid.MessageID)(&post.RootID)
	}
	return converted
}

func (mc *MattermostClient) convertAttachment(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, file *mattermost.FileInfo) *bridgev2.ConvertedMessagePart {
	log := zerolog.Ctx(ctx).With().Str("file_id", file.ID).Logger()
	part := func(content *event.MessageEventContent) *bridgev2.ConvertedMessagePart {
		return &bridgev2.ConvertedMessagePart{
			ID:      networkid.PartID(file.ID),
			Type:    event.EventMessage,
			Content: content,
		}
	}
	failed := func(err error, what string) *bridgev2.ConvertedMessagePart {
		log.Err(err).Msg(what)
		name := file.Name
		if name == "" {
			name = "attachment"
		}
		return part(&event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    fmt.Sprintf("%s could not be bridged: %s", name, what),
		})
	}
	// The posted event normally carries the file metadata; a bare ID means it didn't.
	if file.Name == "" {
		info, err := mc.Client.GetFileInfo(ctx, file.ID)
		if err != nil {
			return failed(err, "failed to get file info")
		}
		file = info
	}
	maxSize := mc.Main.matrixMaxFileSize()
	if file.Size > maxSize {
		err := fmt.Errorf("%.1f MiB is larger than the homeserver's limit of %.1f MiB", float64(file.Size)/1024/1024, float64(maxSize)/1024/1024)
		return failed(err, "file is too large for Matrix")
	}
	data, err := mc.Client.GetFile(ctx, file.ID, maxSize)
	if err != nil {
		return failed(err, "failed to download file")
	}
	mimeType := file.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	content := &event.MessageEventContent{
		MsgType:  messageTypeForMime(mimeType),
		Body:     file.Name,
		FileName: file.Name,
		Info: &event.FileInfo{
			MimeType: mimeType,
			Size:     len(data),
			Width:    file.Width,
			Height:   file.Height,
		},
	}
	url, encrypted, err := intent.UploadMedia(ctx, portal.MXID, data, file.Name, mimeType)
	if err != nil {
		return failed(err, "failed to upload file to Matrix")
	}
	if encrypted != nil {
		content.File = encrypted
	} else {
		content.URL = url
	}
	return part(content)
}

// messageTypeForMime picks the Matrix msgtype that shows the file best: inline for images,
// a player for audio and video, a download for everything else.
func messageTypeForMime(mimeType string) event.MessageType {
	switch strings.SplitN(mimeType, "/", 2)[0] {
	case "image":
		return event.MsgImage
	case "video":
		return event.MsgVideo
	case "audio":
		return event.MsgAudio
	default:
		return event.MsgFile
	}
}

// Matrix → Mattermost

// convertMatrixMedia uploads the message's file to Mattermost and makes the post that carries
// it, with the caption as the post text.
func (mc *MattermostClient) convertMatrixMedia(ctx context.Context, msg *bridgev2.MatrixMessage) (*mattermost.Post, error) {
	content := msg.Content
	data, err := mc.Main.Bridge.Bot.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgev2.ErrMediaDownloadFailed, err)
	}
	info, err := mc.Client.UploadFile(ctx, string(msg.Portal.ID), matrixFileName(content), data)
	if err != nil {
		return nil, bridgev2.WrapErrorInStatus(fmt.Errorf("failed to upload file to Mattermost: %w", err)).WithErrorAsMessage().WithSendNotice(true)
	}
	return mediaPost(ctx, msg, info.ID, mc.getMattermostUsername), nil
}

// mediaPost is the post for a media message whose file is already uploaded as fileID.
func mediaPost(ctx context.Context, msg *bridgev2.MatrixMessage, fileID string, getUsername usernameLookup) *mattermost.Post {
	content := msg.Content
	post := newPost(msg, matrixToMarkdown(ctx, content.GetCaption(), content.GetFormattedCaption(), getUsername))
	post.FileIDs = []string{fileID}
	return post
}

// matrixFileName is the name to upload under. Mattermost shows it and picks the file type from
// its extension, so a nameless file (a sticker, a pasted image) gets one from its mime type.
func matrixFileName(content *event.MessageEventContent) string {
	name := content.GetFileName()
	if name == "" {
		name = "file"
	}
	if strings.Contains(name, ".") || content.Info == nil || content.Info.MimeType == "" {
		return name
	}
	if exts, _ := mime.ExtensionsByType(content.Info.MimeType); len(exts) > 0 {
		return name + exts[0]
	}
	return name
}

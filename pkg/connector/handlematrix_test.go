package connector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func noUsernames(context.Context, id.UserID) string {
	return ""
}

func makeMatrixMessage(content *event.MessageEventContent, threadRoot *database.Message) *bridgev2.MatrixMessage {
	return &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Content: content,
			Portal:  &bridgev2.Portal{Portal: &database.Portal{PortalKey: networkid.PortalKey{ID: "channel1"}}},
		},
		ThreadRoot: threadRoot,
	}
}

func TestConvertMatrixMessage(t *testing.T) {
	post, err := convertMatrixMessage(context.Background(), makeMatrixMessage(&event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          "hi there, see link",
		Format:        event.FormatHTML,
		FormattedBody: `<strong>hi</strong> <u>there</u>, see <a href="https://example.org">link</a>`,
	}, nil), noUsernames)
	require.NoError(t, err)
	assert.Equal(t, "channel1", post.ChannelID)
	assert.Equal(t, "**hi** there, see [link](https://example.org)", post.Message)
	assert.Empty(t, post.RootID)

	post, err = convertMatrixMessage(context.Background(), makeMatrixMessage(&event.MessageEventContent{
		MsgType: event.MsgNotice,
		Body:    "plain *as typed*",
	}, &database.Message{ID: "root1"}), noUsernames)
	require.NoError(t, err)
	assert.Equal(t, "plain *as typed*", post.Message)
	assert.Equal(t, "root1", post.RootID)

	// A Matrix thread started on a message that is itself a Mattermost thread reply
	post, err = convertMatrixMessage(context.Background(), makeMatrixMessage(&event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    "nested",
	}, &database.Message{ID: "reply1", ThreadRoot: "root1"}), noUsernames)
	require.NoError(t, err)
	assert.Equal(t, "root1", post.RootID)

	for _, msgType := range []event.MessageType{event.MsgImage, event.MsgFile, event.MsgEmote} {
		_, err = convertMatrixMessage(context.Background(), makeMatrixMessage(&event.MessageEventContent{MsgType: msgType, Body: "x"}, nil), noUsernames)
		assert.ErrorIs(t, err, bridgev2.ErrUnsupportedMessageType, msgType)
	}
}

func TestConvertMatrixMentions(t *testing.T) {
	usernames := func(_ context.Context, mxid id.UserID) string {
		if mxid == "@mattermost_u1:example.org" {
			return "alice"
		}
		return ""
	}
	post, err := convertMatrixMessage(context.Background(), makeMatrixMessage(&event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          "Alice and Bob: hi",
		Format:        event.FormatHTML,
		FormattedBody: `<a href="https://matrix.to/#/@mattermost_u1:example.org">Alice</a> and <a href="https://matrix.to/#/@bob:example.org">Bob</a>: hi`,
	}, nil), usernames)
	require.NoError(t, err)
	assert.Equal(t, "@alice and Bob: hi", post.Message)
}

func TestMediaPost(t *testing.T) {
	// A caption is the body when it differs from the file name.
	post := mediaPost(context.Background(), makeMatrixMessage(&event.MessageEventContent{
		MsgType:       event.MsgImage,
		Body:          "look at *this*",
		FileName:      "cat.png",
		Format:        event.FormatHTML,
		FormattedBody: "look at <em>this</em>",
	}, &database.Message{ID: "root1"}), "file1", noUsernames)
	assert.Equal(t, []string{"file1"}, post.FileIDs)
	assert.Equal(t, "look at _this_", post.Message)
	assert.Equal(t, "root1", post.RootID)

	// No caption: the body is just the file name.
	post = mediaPost(context.Background(), makeMatrixMessage(&event.MessageEventContent{
		MsgType: event.MsgFile,
		Body:    "report.pdf",
	}, nil), "file2", noUsernames)
	assert.Equal(t, []string{"file2"}, post.FileIDs)
	assert.Empty(t, post.Message)
}

func TestMatrixFileName(t *testing.T) {
	assert.Equal(t, "cat.png", matrixFileName(&event.MessageEventContent{Body: "cat.png"}))
	assert.Equal(t, "cat.png", matrixFileName(&event.MessageEventContent{Body: "caption", FileName: "cat.png"}))
	assert.Equal(t, "image.png", matrixFileName(&event.MessageEventContent{Body: "image", Info: &event.FileInfo{MimeType: "image/png"}}))
	assert.Equal(t, "file.pdf", matrixFileName(&event.MessageEventContent{Info: &event.FileInfo{MimeType: "application/pdf"}}))
	assert.Equal(t, "file", matrixFileName(&event.MessageEventContent{}))
}

package connector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"maunium.net/go/mautrix/event"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

func TestConvertPostText(t *testing.T) {
	converted := convertPostText(&mattermost.Post{Message: "**hi** there"})
	require.Len(t, converted.Parts, 1)
	content := converted.Parts[0].Content
	assert.Equal(t, event.MsgText, content.MsgType)
	assert.Equal(t, "**hi** there", content.Body)
	assert.Equal(t, "<strong>hi</strong> there", content.FormattedBody)
	assert.Nil(t, converted.ThreadRoot)

	// A file-only post has no text part; the attachments are added by convertPost.
	converted = convertPostText(&mattermost.Post{RootID: "root", FileIDs: []string{"f1"}})
	assert.Empty(t, converted.Parts)
	require.NotNil(t, converted.ThreadRoot)
	assert.EqualValues(t, "root", *converted.ThreadRoot)
}

func TestPostFiles(t *testing.T) {
	post := &mattermost.Post{
		FileIDs:  []string{"f1", "f2"},
		Metadata: &mattermost.PostMetadata{Files: []*mattermost.FileInfo{{ID: "f1", Name: "a.png", MimeType: "image/png"}}},
	}
	files := post.Files()
	require.Len(t, files, 2)
	assert.Equal(t, "a.png", files[0].Name)
	assert.Equal(t, "f2", files[1].ID)
	assert.Empty(t, files[1].Name, "an ID without metadata is left for GetFileInfo")

	assert.Empty(t, (&mattermost.Post{}).Files())
}

func TestMessageTypeForMime(t *testing.T) {
	for mimeType, expected := range map[string]event.MessageType{
		"image/png":                event.MsgImage,
		"video/mp4":                event.MsgVideo,
		"audio/ogg":                event.MsgAudio,
		"application/pdf":          event.MsgFile,
		"application/octet-stream": event.MsgFile,
		"":                         event.MsgFile,
	} {
		assert.Equal(t, expected, messageTypeForMime(mimeType), mimeType)
	}
}

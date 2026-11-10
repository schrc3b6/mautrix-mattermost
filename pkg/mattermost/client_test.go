package mattermost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeServerURL(t *testing.T) {
	for input, expected := range map[string]string{
		"chat.example.org":              "https://chat.example.org",
		" https://chat.example.org/ ":   "https://chat.example.org",
		"http://localhost:8065":         "http://localhost:8065",
		"https://example.org/mm/?x=1#y": "https://example.org/mm",
	} {
		actual, err := NormalizeServerURL(input)
		require.NoError(t, err, input)
		assert.Equal(t, expected, actual, input)
	}
	for _, input := range []string{"ftp://example.org", "https://"} {
		_, err := NormalizeServerURL(input)
		assert.Error(t, err, input)
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if r.URL.Path != "/api/v4/users/login" || body["password"] != "correct" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"id":"api.user.login.invalid_credentials_email_username","message":"Enter a valid email or username and/or password."}`))
			return
		}
		w.Header().Set("Token", "session-token")
		_, _ = w.Write([]byte(`{"id":"u1","username":"robin","first_name":"Robin"}`))
	}))
	defer srv.Close()

	cli := &Client{ServerURL: srv.URL}
	_, err := cli.Login(context.Background(), "robin", "wrong")
	require.Error(t, err)
	assert.True(t, IsUnauthorized(err))
	assert.Empty(t, cli.Token)

	user, err := cli.Login(context.Background(), "robin", "correct")
	require.NoError(t, err)
	assert.Equal(t, "session-token", cli.Token)
	assert.Equal(t, "u1", user.ID)
	assert.Equal(t, "Robin", user.DisplayName())
}

func TestCreatePost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v4/posts", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, map[string]any{
			"channel_id":      "c1",
			"message":         "hi",
			"root_id":         "r1",
			"pending_post_id": "txn1",
		}, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p1","create_at":1760000000000,"user_id":"u1","channel_id":"c1","root_id":"r1","message":"hi","pending_post_id":"txn1"}`))
	}))
	defer srv.Close()

	post, err := (&Client{ServerURL: srv.URL}).CreatePost(context.Background(), &Post{ChannelID: "c1", Message: "hi", RootID: "r1", PendingPostID: "txn1"})
	require.NoError(t, err)
	assert.Equal(t, "p1", post.ID)
	assert.Equal(t, "u1", post.UserID)
	assert.EqualValues(t, 1760000000000, post.CreateAt)
}

func TestListen(t *testing.T) {
	post := Post{ID: "p1", ChannelID: "c1", UserID: "u2", Message: "hello", CreateAt: 1760000000000}
	postJSON, _ := json.Marshal(post)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer conn.CloseNow()
		ctx := r.Context()
		hello, _ := json.Marshal(map[string]any{"event": "hello", "data": map[string]string{}, "seq": 0})
		data, _ := json.Marshal(PostedData{ChannelType: ChannelOpen, Post: string(postJSON)})
		posted, _ := json.Marshal(WebSocketEvent{Event: EventPosted, Data: data, Seq: 1})
		_ = conn.Write(ctx, websocket.MessageText, hello)
		_ = conn.Write(ctx, websocket.MessageText, posted)
		_, _, _ = conn.Read(ctx)
	}))
	defer srv.Close()

	err := (&Client{ServerURL: srv.URL, Token: "bad"}).Listen(context.Background(), func() {}, func(*WebSocketEvent) {})
	assert.True(t, IsUnauthorized(err), "expected 401, got %v", err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connected := false
	var received *Post
	err = (&Client{ServerURL: srv.URL, Token: "good"}).Listen(ctx, func() { connected = true }, func(evt *WebSocketEvent) {
		var data PostedData
		require.NoError(t, json.Unmarshal(evt.Data, &data))
		received, err = data.ParsePost()
		require.NoError(t, err)
		cancel()
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, connected)
	assert.Equal(t, &post, received)
}

func TestListenDetectsRevokedSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/users/me" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer conn.CloseNow()
		// Like Mattermost, keep the socket open without sending anything.
		_, _, _ = conn.Read(r.Context())
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := (&Client{ServerURL: srv.URL, Token: "revoked", SessionCheckInterval: 50 * time.Millisecond}).Listen(ctx, func() {}, func(*WebSocketEvent) {})
	assert.True(t, IsUnauthorized(err), "expected 401, got %v", err)
}

func TestOtherDirectMessageUserID(t *testing.T) {
	dm := &Channel{Type: ChannelDirect, Name: "aaaaaaaaaaaaaaaaaaaaaaaaaa__bbbbbbbbbbbbbbbbbbbbbbbbbb"}
	other, ok := dm.OtherDirectMessageUserID("aaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.True(t, ok)
	assert.Equal(t, "bbbbbbbbbbbbbbbbbbbbbbbbbb", other)
	other, ok = dm.OtherDirectMessageUserID("bbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.True(t, ok)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaa", other)
	_, ok = dm.OtherDirectMessageUserID("cccccccccccccccccccccccccc")
	assert.False(t, ok, "not a member")

	self := &Channel{Type: ChannelDirect, Name: "aaaaaaaaaaaaaaaaaaaaaaaaaa__aaaaaaaaaaaaaaaaaaaaaaaaaa"}
	other, ok = self.OtherDirectMessageUserID("aaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.True(t, ok)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaa", other, "DM with yourself")

	_, ok = (&Channel{Type: ChannelGroup, Name: "x__y"}).OtherDirectMessageUserID("x")
	assert.False(t, ok, "not a DM")
	_, ok = (&Channel{Type: ChannelDirect, Name: "town-square"}).OtherDirectMessageUserID("x")
	assert.False(t, ok, "unexpected name")
}

func TestGetChannelMembers(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v4/channels/ch1/members", r.URL.Path)
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		var members []ChannelMember
		if page == "0" {
			for i := 0; i < channelMembersPerPage; i++ {
				members = append(members, ChannelMember{ChannelID: "ch1", UserID: "full"})
			}
		} else {
			members = []ChannelMember{{ChannelID: "ch1", UserID: "last"}}
		}
		require.NoError(t, json.NewEncoder(w).Encode(members))
	}))
	defer srv.Close()

	members, err := (&Client{ServerURL: srv.URL}).GetChannelMembers(context.Background(), "ch1")
	require.NoError(t, err)
	assert.Len(t, members, channelMembersPerPage+1)
	assert.Equal(t, "last", members[channelMembersPerPage].UserID)
	assert.Equal(t, []string{"0", "1"}, pages)
}

func TestCreateDirectAndGroupChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var userIDs []string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&userIDs))
		switch r.URL.Path {
		case "/api/v4/channels/direct":
			assert.Equal(t, []string{"me", "other"}, userIDs)
			_, _ = w.Write([]byte(`{"id":"dm1","type":"D","name":"me__other"}`))
		case "/api/v4/channels/group":
			assert.Equal(t, []string{"me", "a", "b"}, userIDs)
			_, _ = w.Write([]byte(`{"id":"gm1","type":"G","display_name":"a, b, me"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cli := &Client{ServerURL: srv.URL}
	dm, err := cli.CreateDirectChannel(context.Background(), "me", "other")
	require.NoError(t, err)
	assert.Equal(t, "dm1", dm.ID)
	assert.Equal(t, ChannelDirect, dm.Type)

	gm, err := cli.CreateGroupChannel(context.Background(), []string{"me", "a", "b"})
	require.NoError(t, err)
	assert.Equal(t, "gm1", gm.ID)
	assert.Equal(t, "a, b, me", gm.DisplayName)
}

func TestFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v4/files":
			require.NoError(t, r.ParseMultipartForm(1<<20))
			assert.Equal(t, "c1", r.FormValue("channel_id"))
			file, header, err := r.FormFile("files")
			require.NoError(t, err)
			defer file.Close()
			data, _ := io.ReadAll(file)
			assert.Equal(t, "hello.txt", header.Filename)
			assert.Equal(t, "hello", string(data))
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"file_infos":[{"id":"f1","name":"hello.txt","size":5,"mime_type":"text/plain"}],"client_ids":[]}`))
		case "GET /api/v4/files/f1/info":
			_, _ = w.Write([]byte(`{"id":"f1","name":"hello.txt","size":5,"mime_type":"text/plain","width":0,"height":0}`))
		case "GET /api/v4/files/f1":
			_, _ = w.Write([]byte("hello"))
		case "GET /api/v4/config/client":
			assert.Equal(t, "old", r.URL.Query().Get("format"))
			_, _ = w.Write([]byte(`{"MaxFileSize":"52428800","SiteName":"Test"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cli := &Client{ServerURL: srv.URL, Token: "t"}
	ctx := context.Background()

	info, err := cli.UploadFile(ctx, "c1", "hello.txt", []byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, "f1", info.ID)
	assert.Equal(t, "text/plain", info.MimeType)

	info, err = cli.GetFileInfo(ctx, "f1")
	require.NoError(t, err)
	assert.Equal(t, "hello.txt", info.Name)
	assert.EqualValues(t, 5, info.Size)

	data, err := cli.GetFile(ctx, "f1", 1<<20)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
	_, err = cli.GetFile(ctx, "f1", 3)
	assert.Error(t, err, "a file over the limit is refused")

	size, err := cli.MaxFileSize(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 52428800, size)
}

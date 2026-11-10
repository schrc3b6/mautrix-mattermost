package connector

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/robinchoice/mautrix-mattermost/pkg/mattermost"
)

type MattermostClient struct {
	Main      *MattermostConnector
	UserLogin *bridgev2.UserLogin
	Client    *mattermost.Client

	ownUserID string
	loggedIn  atomic.Bool
	stop      atomic.Pointer[context.CancelFunc]
	stopped   atomic.Pointer[chan struct{}]
	// The server's upload limit, read once on connect; 0 until then.
	maxFileSize atomic.Int64
}

// mattermostMaxFileSize is the largest file the bridge will upload to Mattermost.
func (mc *MattermostClient) mattermostMaxFileSize() int64 {
	if size := mc.maxFileSize.Load(); size > 0 {
		return size
	}
	return mattermost.DefaultMaxFileSize
}

var _ bridgev2.NetworkAPI = (*MattermostClient)(nil)

func (mc *MattermostConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	meta := login.Metadata.(*UserLoginMetadata)
	client := &MattermostClient{
		Main:      mc,
		UserLogin: login,
		Client: &mattermost.Client{
			ServerURL: meta.ServerURL,
			Token:     meta.Token,
			HTTP:      &http.Client{Timeout: 60 * time.Second},
			UserAgent: mautrix.DefaultUserAgent,
		},
		ownUserID: parseUserLoginID(login.ID),
	}
	client.loggedIn.Store(meta.Token != "")
	login.Client = client
	return nil
}

func (mc *MattermostClient) Connect(ctx context.Context) {
	if !mc.loggedIn.Load() {
		mc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateBadCredentials, Error: "mm-not-logged-in"})
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	if old := mc.stop.Swap(&cancel); old != nil {
		(*old)()
	}
	done := make(chan struct{})
	mc.stopped.Store(&done)
	go func() {
		defer close(done)
		mc.loadMaxFileSize(ctx)
		mc.listenLoop(ctx)
	}()
}

func (mc *MattermostClient) loadMaxFileSize(ctx context.Context) {
	size, err := mc.Client.MaxFileSize(ctx)
	if err != nil {
		mc.UserLogin.Log.Warn().Err(err).Msg("Failed to read the server's upload limit, assuming the default")
		return
	}
	mc.maxFileSize.Store(size)
}

const (
	minReconnectDelay = 2 * time.Second
	maxReconnectDelay = 5 * time.Minute
)

func (mc *MattermostClient) listenLoop(ctx context.Context) {
	log := mc.UserLogin.Log.With().Str("action", "listen").Logger()
	delay := minReconnectDelay
	for {
		mc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting})
		connectedAt := time.Time{}
		err := mc.Client.Listen(ctx, func() {
			connectedAt = time.Now()
			log.Info().Msg("Connected to Mattermost websocket")
			mc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		}, func(evt *mattermost.WebSocketEvent) {
			mc.handleEvent(log.WithContext(ctx), evt)
		})
		if ctx.Err() != nil {
			return
		} else if mattermost.IsUnauthorized(err) {
			log.Warn().Err(err).Msg("Session token was rejected")
			mc.loggedIn.Store(false)
			mc.UserLogin.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateBadCredentials,
				Error:      "mm-session-invalid",
				Message:    "Mattermost session expired, please log in again",
			})
			return
		}
		if !connectedAt.IsZero() && time.Since(connectedAt) > maxReconnectDelay {
			delay = minReconnectDelay
		}
		log.Warn().Err(err).Dur("retry_in", delay).Msg("Websocket disconnected")
		mc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateTransientDisconnect,
			Error:      "mm-websocket-disconnected",
			Message:    err.Error(),
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, maxReconnectDelay)
	}
}

func (mc *MattermostClient) Disconnect() {
	if cancel := mc.stop.Swap(nil); cancel != nil {
		(*cancel)()
	}
	if done := mc.stopped.Load(); done != nil {
		select {
		case <-*done:
		case <-time.After(5 * time.Second):
			mc.UserLogin.Log.Warn().Msg("Timed out waiting for websocket to stop")
		}
	}
}

func (mc *MattermostClient) IsLoggedIn() bool {
	return mc.loggedIn.Load()
}

func (mc *MattermostClient) LogoutRemote(ctx context.Context) {
	mc.Disconnect()
	// The bridge created this session with the user's password, so it's ours to end.
	if err := mc.Client.Logout(ctx); err != nil && !mattermost.IsUnauthorized(err) {
		mc.UserLogin.Log.Warn().Err(err).Msg("Failed to end Mattermost session")
	}
	mc.loggedIn.Store(false)
}

func (mc *MattermostClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return string(userID) == mc.ownUserID
}

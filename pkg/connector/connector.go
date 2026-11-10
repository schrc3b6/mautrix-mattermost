package connector

import (
	"context"
	"sync/atomic"

	"maunium.net/go/mautrix/bridgev2"
)

type MattermostConnector struct {
	Bridge *bridgev2.Bridge
	Config Config

	// The homeserver's upload limit, from bridgev2 soon after start; 0 until then.
	maxFileSize atomic.Int64
}

var (
	_ bridgev2.NetworkConnector      = (*MattermostConnector)(nil)
	_ bridgev2.MaxFileSizeingNetwork = (*MattermostConnector)(nil)
)

func (mc *MattermostConnector) SetMaxFileSize(maxSize int64) {
	mc.maxFileSize.Store(maxSize)
}

// matrixMaxFileSize is the largest attachment the bridge will upload to Matrix.
func (mc *MattermostConnector) matrixMaxFileSize() int64 {
	if size := mc.maxFileSize.Load(); size > 0 {
		return size
	}
	return defaultMatrixMaxFileSize
}

func (mc *MattermostConnector) Init(bridge *bridgev2.Bridge) {
	mc.Bridge = bridge
}

func (mc *MattermostConnector) Start(ctx context.Context) error {
	return nil
}

func (mc *MattermostConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "Mattermost",
		NetworkURL:           "https://mattermost.com/",
		NetworkID:            "mattermost",
		BeeperBridgeType:     "mattermost",
		DefaultPort:          29345,
		DefaultCommandPrefix: "!mm",
	}
}

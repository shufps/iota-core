package core

import (
	"encoding/json"

	"github.com/iotaledger/iota.go/v4/nodeclient/apimodels"
)

//nolint:unparam // we have no error case right now
func info() (*apimodels.InfoResponse, error) {
	clSnapshot := deps.Protocol.MainEngineInstance().Clock.Snapshot()
	syncStatus := deps.Protocol.SyncManager.SyncStatus()
	metrics := deps.MetricsTracker.NodeMetrics()
	protoParams := deps.Protocol.CurrentAPI().ProtocolParameters()

	protoParamsBytes, err := deps.Protocol.CurrentAPI().JSONEncode(protoParams)
	if err != nil {
		return nil, err
	}
	protoParamsJSONRaw := json.RawMessage(protoParamsBytes)

	return &apimodels.InfoResponse{
		Name:    deps.AppInfo.Name,
		Version: deps.AppInfo.Version,
		Status: &apimodels.InfoResNodeStatus{
			IsHealthy:                   syncStatus.NodeSynced,
			AcceptedTangleTime:          uint64(clSnapshot.AcceptedTime.UnixNano()),
			RelativeAcceptedTangleTime:  uint64(clSnapshot.RelativeAcceptedTime.UnixNano()),
			ConfirmedTangleTime:         uint64(clSnapshot.ConfirmedTime.UnixNano()),
			RelativeConfirmedTangleTime: uint64(clSnapshot.RelativeConfirmedTime.UnixNano()),
			LatestCommitmentID:          syncStatus.LatestCommitment.ID(),
			LatestFinalizedSlot:         syncStatus.LatestFinalizedSlot,
			LatestAcceptedBlockSlot:     syncStatus.LastAcceptedBlockSlot,
			LatestConfirmedBlockSlot:    syncStatus.LastConfirmedBlockSlot,
			PruningSlot:                 syncStatus.LatestPrunedSlot,
		},
		Metrics: &apimodels.InfoResNodeMetrics{
			BlocksPerSecond:          metrics.BlocksPerSecond,
			ConfirmedBlocksPerSecond: metrics.ConfirmedBlocksPerSecond,
			ConfirmationRate:         metrics.ConfirmedRate,
		},
		SupportedProtocolVersions: deps.Protocol.SupportedVersions(),
		ProtocolParameters:        &protoParamsJSONRaw,
		Features:                  features,
	}, nil
}

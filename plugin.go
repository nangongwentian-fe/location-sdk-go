// Package locationsdk connects applications to a locally installed location engine.
package locationsdk

import (
	"context"
	"errors"

	"github.com/hashicorp/go-plugin"
	"github.com/nangongwentian-fe/location-sdk-go/protocol"
	"google.golang.org/grpc"
)

const ProtocolVersion = 1
const StateFormat = "location-state-v1"
const PluginName = "location"

var Handshake = plugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: "LOCATION_ENGINE_PLUGIN", MagicCookieValue: "location-engine-v1"}

// Plugin is shared by the engine executable and its client. It contains no algorithms.
type Plugin struct {
	plugin.NetRPCUnsupportedPlugin
	Implementation protocol.EngineServer
}

func (p *Plugin) GRPCServer(_ *plugin.GRPCBroker, server *grpc.Server) error {
	if p.Implementation == nil {
		return errors.New("engine server is required")
	}
	protocol.RegisterEngineServer(server, p.Implementation)
	return nil
}
func (p *Plugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, connection *grpc.ClientConn) (interface{}, error) {
	return protocol.NewEngineClient(connection), nil
}

package locationsdk

import (
	"context"
	"github.com/hashicorp/go-plugin"
	"github.com/nangongwentian-fe/location-sdk-go/protocol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
	"time"
)

// Serve runs a single-host local engine. Losing the authenticated host connection
// terminates the server so an abruptly terminated host does not leave an orphan.
func Serve(implementation protocol.EngineServer) {
	plugin.Serve(&plugin.ServeConfig{HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{PluginName: &Plugin{Implementation: implementation}},
		GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
			h := &hostConnection{}
			server := grpc.NewServer(append(options, grpc.StatsHandler(h))...)
			h.stop = server.Stop
			h.startup = time.AfterFunc(30*time.Second, server.Stop)
			return server
		},
	})
}

type hostConnection struct {
	stop    func()
	startup *time.Timer
}

func (*hostConnection) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context   { return ctx }
func (*hostConnection) HandleRPC(context.Context, stats.RPCStats)                         {}
func (*hostConnection) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (h *hostConnection) HandleConn(_ context.Context, event stats.ConnStats) {
	switch event.(type) {
	case *stats.ConnBegin:
		h.startup.Stop()
	case *stats.ConnEnd:
		go h.stop()
	}
}

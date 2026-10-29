// Test transport only: this executable contains no location algorithms.
package main

import (
	"context"
	sdk "github.com/nangongwentian-fe/location-sdk-go"
	p "github.com/nangongwentian-fe/location-sdk-go/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type service struct{ p.UnimplementedEngineServer }

func (*service) Info(context.Context, *p.InfoRequest) (*p.InfoResponse, error) {
	if strings.Contains(filepath.Base(os.Args[0]), "frozen") {
		_ = exec.Command("/bin/kill", "-STOP", strconv.Itoa(os.Getpid())).Run()
	}
	info := &p.InfoResponse{Version: "test", ProtocolVersion: sdk.ProtocolVersion, StateFormats: []string{sdk.StateFormat}}
	if strings.Contains(filepath.Base(os.Args[0]), "wrong-protocol") {
		info.ProtocolVersion++
	}
	if strings.Contains(filepath.Base(os.Args[0]), "wrong-state") {
		info.StateFormats = []string{"unsupported"}
	}
	return info, nil
}
func (*service) ValidateConfig(ctx context.Context, in *p.ConfigRequest) (*p.ConfigResponse, error) {
	if in.Kind == "pid" {
		return &p.ConfigResponse{ParametersJson: []byte(strconv.Itoa(os.Getpid()))}, nil
	}
	if in.Kind == "wait" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if in.Kind == "crash" {
		os.Exit(3)
	}
	return &p.ConfigResponse{ParametersJson: in.ParametersJson}, nil
}
func main() { sdk.Serve(&service{}) }

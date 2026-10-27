// Run with an absolute engine path and its independently verified manifest.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	sdk "github.com/nangongwentian-fe/location-sdk-go"
	p "github.com/nangongwentian-fe/location-sdk-go/protocol"
	"os"
)

func ptr[T any](v T) *T { return &v }
func main() {
	if len(os.Args) != 2 {
		panic("usage: smoke /absolute/path/location-engine")
	}
	var manifest struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	raw, err := os.ReadFile(os.Args[1] + ".json")
	check(err)
	check(json.Unmarshal(raw, &manifest))
	ctx := context.Background()
	client, err := sdk.Open(ctx, sdk.Options{Path: os.Args[1], Version: manifest.Version, SHA256: manifest.SHA256})
	check(err)
	defer client.Close()
	locations, err := client.EvaluateLocations(ctx, &p.LocationRequest{DeviceId: "example", Ruleset: "location-pipeline-v1", ParametersJson: []byte(`{"pipeline":[{"id":"invalid-coordinate"},{"id":"water-last-land"},{"id":"raw-passthrough"}]}`), Samples: []*p.Sample{{SampleId: ptr(int64(1)), Time: 1800000000000, Lat: ptr(22.3), Lon: ptr(114.2), Spatial: &p.Spatial{Classification: ptr("land")}}}})
	check(err)
	if len(locations.Decisions) != 1 || locations.Decisions[0].Action != "accept" {
		panic("unexpected location decision")
	}
	alarms, err := client.EvaluateAlarms(ctx, &p.AlarmRequest{Now: 1800000000000, Items: []*p.AlarmInput{{TemplateCode: "suspected-stationary", TemplateVersion: 1, ParametersJson: []byte(`{"triggerAfterMinutes":10,"repeatEveryMinutes":10}`), NewEpisodeKey: "example-episode", Device: &p.AlarmDevice{DeviceId: "example", InCatalog: true, InRuleScope: true, OnlineStatus: ptr("online"), MotionStatus: ptr("stationary")}}}})
	check(err)
	if len(alarms.Results) != 1 || alarms.Results[0].State == nil {
		panic("unexpected alarm state")
	}
	fmt.Println("location, alarm and verified engine handshake passed")
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}

package integration

import (
	"reflect"
	"testing"
	"time"
)

func TestStatuses(t *testing.T) {
	ran := Run{ID: 7, StartedAt: time.Unix(0, 0), Trigger: "push", Failed: 1}
	got := statuses(
		map[string]string{"inventory": inventoryYAML},
		map[string]string{"service": "Service"},
		map[string]Run{"inventory": ran, "deleted": {ID: 1}},
		map[string]map[string]int{"inventory": {"service": 3, "legacy": 2}},
	)

	want := []Status{{
		Integration: "inventory",
		LastRun:     &ran,
		// legacy is no longer mapped, but its entities are still the integration's.
		Feeds: []Feed{{Blueprint: "service", Title: "Service", Entities: 3}, {Blueprint: "legacy", Title: "legacy", Entities: 2}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statuses() = %+v, want %+v", got, want)
	}

	never := statuses(map[string]string{"inventory": inventoryYAML}, nil, nil, nil)
	if never[0].LastRun != nil || never[0].Feeds[0] != (Feed{Blueprint: "service", Title: "service"}) {
		t.Errorf("never run = %+v", never)
	}
}

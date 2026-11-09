package connection

import (
	"reflect"
	"testing"
	"time"

	"github.com/dvjn/goldenportal/internal/blueprint"
)

func TestStatuses(t *testing.T) {
	service, _ := blueprint.Parse(serviceYAML)
	published := map[string]blueprint.Spec{"service": service}
	ran := Run{ID: 7, StartedAt: time.Unix(0, 0), Trigger: "push", Failed: 1}
	got := statuses(
		map[string]string{"inventory": inventoryYAML},
		published,
		map[string]Run{"inventory": ran, "deleted": {ID: 1}},
		map[string]map[string]int{"inventory": {"service": 3, "legacy": 2}},
	)

	want := []Status{{
		Connection: "inventory",
		LastRun:    &ran,
		// legacy is no longer read, but its entities are still the connection's.
		Feeds:   []Feed{{Blueprint: "service", Title: "Service", Entities: 3}, {Blueprint: "legacy", Title: "legacy", Entities: 2}},
		Sources: []ReadSource{{Resource: "services", Blueprint: "service", Title: "Service", Mode: blueprint.SourceReconcile}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statuses() = %+v, want %+v", got, want)
	}

	never := statuses(map[string]string{"inventory": inventoryYAML}, published, nil, nil)
	if never[0].LastRun != nil || never[0].Feeds[0] != (Feed{Blueprint: "service", Title: "Service"}) {
		t.Errorf("never run = %+v", never)
	}
}

func TestUsedSources(t *testing.T) {
	service, _ := blueprint.Parse(serviceYAML)
	at := time.Unix(60, 0)
	got := usedSources(service, map[string]string{"inventory": "Inventory"}, map[string]lastRun{"inventory": {at: at, outcome: "warnings"}})
	want := []UsedSource{{Connection: "inventory", Title: "Inventory", Resource: "services", Mode: blueprint.SourceReconcile, LastRunAt: &at, LastRunOutcome: "warnings"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usedSources() = %+v, want %+v", got, want)
	}

	never := usedSources(service, nil, nil)
	if never[0].Title != "inventory" || never[0].LastRunAt != nil {
		t.Errorf("never run = %+v", never)
	}
}

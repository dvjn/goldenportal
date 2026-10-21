package definition

import "slices"

// Field names one property or relation of a blueprint.
type Field struct {
	Blueprint string `json:"blueprint"`
	Name      string `json:"name"`
}

// Effects are what applying a set does to the entities that the current
// set describes.
type Effects struct {
	// RemovedBlueprints lose all their entities.
	RemovedBlueprints []string
	// RemovedIntegrations lose the entities they own.
	RemovedIntegrations []string
	// RemovedProperties lose their values.
	RemovedProperties []Field
	// ChangedProperties keep only the values the new definition accepts.
	ChangedProperties []ChangedProperty
	// RemovedRelations, also those that now link to another blueprint, lose
	// their links.
	RemovedRelations []Field
	// NarrowedRelations now link to one target, so a source that links to
	// more loses its links.
	NarrowedRelations []Field
}

type ChangedProperty struct {
	Field
	Property Property
}

func EffectsOf(from, to Set) Effects {
	var effects Effects
	for _, integration := range from.Integrations() {
		if _, kept := to.Integration(integration.Identifier); !kept {
			effects.RemovedIntegrations = append(effects.RemovedIntegrations, integration.Identifier)
		}
	}
	for _, before := range from.Blueprints() {
		after, kept := to.Blueprint(before.Identifier)
		if !kept {
			effects.RemovedBlueprints = append(effects.RemovedBlueprints, before.Identifier)
			continue
		}
		effects.properties(before, after)
		effects.relations(before, after)
	}

	return effects
}

func (e *Effects) properties(before, after *Blueprint) {
	for key, previous := range before.Properties.All() {
		field := Field{Blueprint: before.Identifier, Name: key}
		current, kept := after.Properties.Get(key)
		switch {
		case !kept:
			e.RemovedProperties = append(e.RemovedProperties, field)
		case current.Type != previous.Type || current.Format != previous.Format || !slices.Equal(current.Enum, previous.Enum):
			e.ChangedProperties = append(e.ChangedProperties, ChangedProperty{Field: field, Property: current})
		}
	}
}

func (e *Effects) relations(before, after *Blueprint) {
	for key, previous := range before.Relations.All() {
		field := Field{Blueprint: before.Identifier, Name: key}
		current, kept := after.Relations.Get(key)
		switch {
		case !kept || current.Target != previous.Target:
			e.RemovedRelations = append(e.RemovedRelations, field)
		case previous.Many && !current.Many:
			e.NarrowedRelations = append(e.NarrowedRelations, field)
		}
	}
}

package dashboardmetrics

import (
	"github.com/iotaledger/hive.go/runtime/event"
)

// Events defines the events of the plugin.
var Events *EventsStruct

type EventsStruct struct {
	// Fired when the component counter per second metric is updated.
	ComponentCounterUpdated *event.Event1[*ComponentCounterUpdatedEvent]

	event.Group[EventsStruct, *EventsStruct]
}

func init() {
	Events = NewEvents()
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (self *EventsStruct) {
	return &EventsStruct{
		ComponentCounterUpdated: event.New1[*ComponentCounterUpdatedEvent](),
	}
})

type ComponentCounterUpdatedEvent struct {
	ComponentStatus map[ComponentType]uint64
}

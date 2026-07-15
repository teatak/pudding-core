package sseresume

type Event struct {
	Seq  int64
	Kind string
}

func ReplayAfter(events []Event, lastEventID int64) []Event {
	var out []Event
	for _, event := range events {
		if event.Seq >= lastEventID {
			out = append(out, event)
		}
	}
	return out
}

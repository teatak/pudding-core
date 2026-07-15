package turncancel

type Status string

const (
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

func Cancel(turns map[string]Status, id string) bool {
	if _, ok := turns[id]; !ok {
		return false
	}
	turns[id] = Completed
	return true
}

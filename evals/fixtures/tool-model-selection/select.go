package toolmodel

type Model struct {
	ID    string
	Tools bool
}

func SelectForCode(models []Model) (Model, bool) {
	if len(models) == 0 {
		return Model{}, false
	}
	return models[0], true
}

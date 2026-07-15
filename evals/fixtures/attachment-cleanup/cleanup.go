package attachmentcleanup

type Input struct {
	AttachmentKeys []string
}

func ReferencedKeys(messages, queued []Input) []string {
	var out []string
	for _, message := range messages {
		out = append(out, message.AttachmentKeys...)
	}
	return out
}

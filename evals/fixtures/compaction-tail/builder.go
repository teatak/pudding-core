package compactiontail

type Message struct {
	Role string
	Text string
}

func EffectiveMessages(messages []Message) []Message {
	for index, message := range messages {
		if message.Role == "summary" {
			return messages[index:]
		}
	}
	return messages
}

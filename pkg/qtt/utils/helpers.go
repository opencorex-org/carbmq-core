package utils

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

func NewPacketID() string {
	return uuid.NewString()
}

func TimestampNow() time.Time {
	return time.Now().UTC()
}

func MustJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return payload
}

func TopicOwner(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) < 2 {
		return ""
	}
	if parts[0] != "device" {
		return ""
	}

	return parts[1]
}

func MatchTopic(filter string, topic string) bool {
	filterParts := strings.Split(filter, "/")
	topicParts := strings.Split(topic, "/")

	for idx := 0; idx < len(filterParts); idx++ {
		if idx >= len(topicParts) {
			return filterParts[idx] == "#"
		}

		switch filterParts[idx] {
		case "#":
			return true
		case "+":
			continue
		default:
			if filterParts[idx] != topicParts[idx] {
				return false
			}
		}
	}

	return len(filterParts) == len(topicParts)
}

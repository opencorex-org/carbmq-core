package encoder

import (
	"encoding/json"
	"io"

	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type JSONEncoder struct {
	encoder *json.Encoder
}

func NewJSONEncoder(writer io.Writer) *JSONEncoder {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)

	return &JSONEncoder{encoder: encoder}
}

func (e *JSONEncoder) Encode(packet qtttypes.Packet) error {
	return e.encoder.Encode(packet)
}

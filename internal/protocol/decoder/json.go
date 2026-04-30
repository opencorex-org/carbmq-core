package decoder

import (
	"encoding/json"
	"io"

	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type JSONDecoder struct {
	decoder *json.Decoder
}

func NewJSONDecoder(reader io.Reader) *JSONDecoder {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	return &JSONDecoder{decoder: decoder}
}

func (d *JSONDecoder) Decode() (qtttypes.Packet, error) {
	var packet qtttypes.Packet
	if err := d.decoder.Decode(&packet); err != nil {
		return qtttypes.Packet{}, err
	}

	return packet, nil
}

package events

import (
	"strings"

	"emperror.dev/errors"
	"github.com/goccy/go-json"

	"github.com/pwindows/phantom-wings/system"
)

type Event struct {
	Topic string
	Data  interface{}
}

type Bus struct {
	*system.SinkPool
}

func NewBus() *Bus {
	return &Bus{
		system.NewSinkPool(),
	}
}

func (b *Bus) Publish(topic string, data interface{}) {
	if strings.Contains(topic, ":") {
		parts := strings.SplitN(topic, ":", 2)
		if len(parts) == 2 {
			topic = parts[0]
		}
	}

	enc, err := json.Marshal(Event{Topic: topic, Data: data})
	if err != nil {
		panic(errors.WithStack(err))
	}
	b.Push(enc)
}

func MustDecode(data []byte) (e Event) {
	if err := DecodeTo(data, &e); err != nil {
		panic(err)
	}
	return
}

func DecodeTo(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, &v); err != nil {
		return errors.Wrap(err, "events: failed to decode byte slice")
	}
	return nil
}
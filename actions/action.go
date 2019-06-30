package actions

import (
	"context"
	"fmt"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/transport"
	"github.com/mitchellh/mapstructure"
)

type Action interface {
	setType(string)
	GetType() string
	Run(context.Context, transport.Connection, config.Config) error
}

type ActionBase struct {
	Type string
}

func (a *ActionBase) setType(t string) {
	a.Type = t
}

func (a *ActionBase) GetType() string {
	return a.Type
}

func initAction(actionType string) (Action, error) {
	var a Action

	switch actionType {
	case "file":
		a = &FileAction{}
	case "apt":
		a = &AptAction{}
	case "service":
		a = &ServiceAction{}
	case "shell":
		a = &ShellAction{}
	case "validate":
		a = &ValidateAction{}
	default:
		return nil, fmt.Errorf("unrecognised action: %s", actionType)
	}

	a.setType(actionType)

	return a, nil
}

// UnmarshalAction decodes an action into one of our action types
// using mapstructure
func UnmarshalAction(actionType string, rawAction interface{}) (Action, error) {
	action, err := initAction(actionType)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise action: %s", err)
	}

	decoder, err := mapstructure.NewDecoder(
		&mapstructure.DecoderConfig{
			// Note: The YAML library only performs a few "weak" conversions
			// according to the YAML specification. We have to do the rest
			// (such as string -> duration) using mapstructure.
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
			),
			// Throw an error if any action fields are not used during
			// the decoding process.
			ErrorUnused: true,
			Result:      &action,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise action decoder: %s", err)
	}

	// Hack: We use the generic mapstructure tag "data" to decode actions which
	// are represented as `key: value` instead of `key: map_of_values`.
	if str, ok := rawAction.(string); ok {
		rawAction = map[string]string{"data": str}
	}

	err = decoder.Decode(rawAction)
	if err != nil {
		return nil, fmt.Errorf("failed to decode action: %s", err)
	}

	return action, nil
}

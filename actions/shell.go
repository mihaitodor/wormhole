package actions

import (
	"context"
	"fmt"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
	"golang.org/x/sync/errgroup"
)

type ShellAction struct {
	ActionBase
	string
}

// UnmarshalYAML is required because go-yaml doesn't support deserialising
// basic types into structs with embedded fields even when using the `inline` flag.
// Details here: https://github.com/go-yaml/yaml/issues/100
func (a *ShellAction) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var shell string
	err := unmarshal(&shell)
	if err != nil {
		return fmt.Errorf("failed to unmarshall shell action: %s", err)
	}

	a.string = shell

	return nil
}

func (a *ShellAction) Run(ctx context.Context, conn *connection.Connection, _ config.Config) error {
	return conn.Exec(ctx, true, func(sess *connection.Session) (error, *errgroup.Group) {
		return sess.Start(a.string), nil
	})
}

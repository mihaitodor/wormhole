package actions

import (
	"context"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/transport"
	"golang.org/x/sync/errgroup"
)

type ShellAction struct {
	ActionBase
	// Use the generic tag "data" because this action is represented
	// as `key: string_value` in the playbook YAML
	Command string `mapstructure:"data"`
}

func (a *ShellAction) Run(ctx context.Context, conn transport.Connection, _ config.Config) error {
	return conn.Exec(ctx, true, func(sess *transport.Session) (error, *errgroup.Group) {
		return sess.Start(a.Command), nil
	})
}

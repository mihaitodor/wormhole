package actions

import (
	"context"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
	"golang.org/x/sync/errgroup"
)

type ShellAction string

func (*ShellAction) GetType() string {
	return "shell"
}

func (a *ShellAction) Run(ctx context.Context, conn *connection.Connection, _ config.Config) error {
	return conn.Exec(ctx, true, func(sess *connection.Session) (error, *errgroup.Group) {
		return sess.Start(string(*a)), nil
	})
}

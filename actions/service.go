package actions

import (
	"context"
	"fmt"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
	"golang.org/x/sync/errgroup"
)

type ServiceAction struct {
	ActionBase
	Name  string
	State string
}

func (a *ServiceAction) Run(ctx context.Context, conn connection.Connection, _ config.Config) error {
	return conn.Exec(ctx, true, func(sess *connection.Session) (error, *errgroup.Group) {
		return sess.Start(fmt.Sprintf("service %s %s", a.Name, a.State)), nil
	})
}

package actions

import (
	"context"
	"fmt"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/transport"
	"golang.org/x/sync/errgroup"
)

type ServiceAction struct {
	ActionBase
	Name  string `mapstructure:"name"`
	State string `mapstructure:"state"`
}

func (a *ServiceAction) Run(ctx context.Context, conn transport.Connection, _ config.Config) error {
	return conn.Exec(ctx, true, func(sess *transport.Session) (error, *errgroup.Group) {
		return sess.Start(fmt.Sprintf("service %s %s", a.Name, a.State)), nil
	})
}

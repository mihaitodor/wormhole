package actions

import (
	"context"
	"fmt"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/transport"
	"golang.org/x/sync/errgroup"
)

type AptAction struct {
	ActionBase
	State string   `mapstructure:"state"`
	Pkg   []string `mapstructure:"pkg"`
}

func (a *AptAction) Run(ctx context.Context, conn transport.Connection, _ config.Config) error {
	// Update package lists first
	err := conn.Exec(ctx, true, func(sess *transport.Session) (error, *errgroup.Group) {
		return sess.Start("apt-get update"), nil
	})
	if err != nil {
		return fmt.Errorf("failed to update package lists: %s", err)
	}

	// Install the requested packages
	for _, pkg := range a.Pkg {
		err = conn.Exec(ctx, true, func(sess *transport.Session) (error, *errgroup.Group) {
			return sess.Start(fmt.Sprintf("apt-get %s -y %s", a.State, pkg)), nil
		})
		if err != nil {
			return fmt.Errorf("failed to install package %q: %s", pkg, err)
		}
	}

	return nil
}

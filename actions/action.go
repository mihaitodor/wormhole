package actions

import (
	"context"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
)

type Action interface {
	GetType() string
	Run(context.Context, *connection.Connection, config.Config) error
}

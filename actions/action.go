package actions

import (
	"context"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/transport"
)

type Action interface {
	SetType(string)
	GetType() string
	Run(context.Context, transport.Connection, config.Config) error
}

type ActionBase struct {
	Type string
}

func (a *ActionBase) SetType(t string) {
	a.Type = t
}

func (a *ActionBase) GetType() string {
	return a.Type
}

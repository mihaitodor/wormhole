package config

import (
	"log"
	"path/filepath"
	"time"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

type Config struct {
	Playbook                 string
	PlaybookFolder           string
	Inventory                string
	ConnectTimeout           time.Duration
	ExecTimeout              time.Duration
	MaxConcurrentConnections int
}

func NewConfing() Config {
	playbook := kingpin.Arg("playbook", "Playbook file.").Required().String()

	inventory := kingpin.Flag("inventory", "Inventory file.").
		Short('i').Default("inventory.yaml").String()

	connectTimeout := kingpin.Flag("connect-timeout", "Connect timeout.").
		Short('c').Default("5s").Duration()

	execTimeout := kingpin.Flag("exec-timeout", "Execution timeout.").
		Short('e').Default("5m").Duration()

	maxConcurrentConnections := kingpin.Flag("max-concurrent-connections", "Max concurrent connections.").
		Short('m').Default("2").Uint()

	kingpin.Parse()

	if *maxConcurrentConnections == 0 {
		log.Fatal("Max concurrent connections needs to be greater than 0")
	}

	return Config{
		Playbook:                 *playbook,
		PlaybookFolder:           filepath.Dir(*playbook),
		Inventory:                *inventory,
		ConnectTimeout:           *connectTimeout,
		ExecTimeout:              *execTimeout,
		MaxConcurrentConnections: int(*maxConcurrentConnections),
	}
}

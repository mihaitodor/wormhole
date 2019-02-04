package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
	"github.com/mihaitodor/wormhole/inventory"
	"github.com/mihaitodor/wormhole/playbook"
	log "github.com/sirupsen/logrus"
)

func Run(ctx context.Context, conf config.Config, playbook *playbook.Playbook, inventory inventory.Inventory) {
	for start := 0; start < len(inventory); start += conf.MaxConcurrentConnections {
		end := start + conf.MaxConcurrentConnections
		if end > len(inventory) {
			end = len(inventory)
		}

		log.Infof("Running playbook on servers: %s",
			strings.Join(inventory[start:end].GetAllServers(nil), ", "),
		)

		// Open a ssh session to each server in the current batch
		var connections []connection.Connection
		for _, server := range inventory[start:end] {
			conn, err := connection.NewConnection(server, conf.ConnectTimeout)
			if err != nil {
				err = fmt.Errorf("Failed to connect to server %q: %s", server.GetAddress(), err)
				server.SetError(err)
				log.Warn(err)
				continue
			}

			connections = append(connections, conn)
		}

		if len(connections) == 0 {
			continue
		}

		var wg sync.WaitGroup
		wg.Add(len(connections))
		for _, conn := range connections {
			go playbook.Run(ctx, &wg, conn, conf)
		}
		wg.Wait()

		// Close ssh clients
		for _, conn := range connections {
			err := conn.Close()
			if err != nil {
				log.Warnf(
					"Failed to close ssh connection to server %q: %s",
					conn.GetAddress(), err,
				)
			}
		}
	}
}

func InitGracefulStop() context.Context {
	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		sig := <-gracefulStop
		log.Warnf("Received signal %q. Exiting as soon as possible!", sig)
		cancel()
	}()

	return ctx
}

func main() {
	conf := config.NewConfing()

	inventory, err := inventory.NewInventory(conf.Inventory)
	if err != nil {
		log.Fatalf("Failed to load inventory: %s", err)
	}

	playbook, err := playbook.NewPlaybook(conf.Playbook)
	if err != nil {
		log.Fatalf("Failed to load playbook: %s", err)
	}

	ctx := InitGracefulStop()

	Run(ctx, conf, playbook, inventory)

	completed := inventory.GetAllCompletedServers()
	if len(completed) > 0 {
		log.Infof("Playbook ran successfully on servers: %s", strings.Join(completed, ", "))
	}

	failed := inventory.GetAllFailedServers()
	if len(failed) > 0 {
		log.Errorf("Playbook failed on servers: %s", strings.Join(failed, ", "))
	}

	select {
	case <-ctx.Done():
		log.Fatalf("Abnormal termination due to: %s", ctx.Err())
	default:
		// Exit normally
	}
}

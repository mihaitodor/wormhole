package transport

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/mihaitodor/wormhole/inventory"
	. "github.com/smartystreets/goconvey/convey"
	"golang.org/x/sync/errgroup"
)

func initServer(address string) (*inventory.Server, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	p, err := strconv.ParseUint(port, 10, 32)
	if err != nil {
		return nil, err
	}

	return &inventory.Server{Host: host, Port: uint(p)}, nil
}

func Test_NewConnection(t *testing.T) {
	Convey("NewConnection()", t, func(c C) {
		// Let the OS select a random port
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			listener, err = net.Listen("tcp6", "[::1]:0")
			So(err, ShouldBeNil)
		}

		server, err := initServer(listener.Addr().String())
		So(err, ShouldBeNil)

		dummySshCommand := "cowsay"
		sshHandlerDone := make(chan struct{})
		dummySshServer := ssh.Server{
			Addr: server.GetAddress(),
			Handler: func(s ssh.Session) {
				c.So(s.Command(), ShouldHaveLength, 1)
				c.So(s.Command()[0], ShouldEqual, dummySshCommand)
				close(sshHandlerDone)
			},
		}

		// Start the dummySshServer in the background
		go func() {
			err := dummySshServer.Serve(listener)
			c.So(err, ShouldEqual, ssh.ErrServerClosed)
		}()
		Reset(func() {
			So(dummySshServer.Shutdown(context.Background()), ShouldBeNil)
		})

		Convey("should set up a connection without timing out", func(c C) {
			var conn Connection
			connReady := make(chan struct{})
			go func() {
				var err error
				conn, err = NewConnection(server, 500*time.Millisecond)
				c.So(err, ShouldBeNil)

				close(connReady)
			}()

			var timeout time.Time
			select {
			case <-connReady:
			case timeout = <-time.After(1 * time.Second):
			}
			So(timeout, ShouldBeZeroValue)

			Reset(func() {
				So(conn.Close(), ShouldBeNil)
			})

			Convey("and set the expected address and host", func() {
				So(conn.GetAddress(), ShouldEqual, server.GetAddress())
				So(conn.GetHost(), ShouldEqual, server.Host)
			})

			Convey("and persist errors to the server", func() {
				dummyError := errors.New("ka-boom!")
				conn.SetError(dummyError)
				So(server.GetError(), ShouldEqual, dummyError)
			})

			Convey("and execute a command on the server", func() {
				err := conn.Exec(context.Background(), false, func(sess *Session) (error, *errgroup.Group) {
					return sess.Start(dummySshCommand), nil
				})
				So(err, ShouldBeNil)

				var timeout time.Time
				select {
				case <-sshHandlerDone:
				case timeout = <-time.After(1 * time.Second):
				}
				So(timeout, ShouldBeZeroValue)
			})
		})

		Convey("should fail to connect on a closed port", func() {
			// Close the underlying listener of dummySshServer
			err := listener.Close()
			So(err, ShouldBeNil)

			_, err = NewConnection(server, 500*time.Millisecond)
			So(err.Error(), ShouldContainSubstring, "connect: connection refused")
		})
	})
}

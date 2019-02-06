package transport

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mihaitodor/wormhole/inventory"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

// Session is a wrapper around ssh.Session
type Session struct {
	sshSess               *ssh.Session
	onceStdinCloser       sync.Once
	stdin                 io.WriteCloser
	sigintHandlerQuitChan chan struct{}
}

// Start starts a remote process in the current session
func (s *Session) Start(cmd string) error {
	return s.sshSess.Start(cmd)
}

// wait blocks until the remote process completes or is cancelled
func (s *Session) wait() error {
	return s.sshSess.Wait()
}

// Stdin returns a pipe to the stdin of the remote process
func (s *Session) Stdin() io.Writer {
	return s.stdin
}

// CloseStdin closes the stdin pipe of the remote process
func (s *Session) CloseStdin() error {
	var err error
	s.onceStdinCloser.Do(func() {
		err = s.stdin.Close()
	})
	return err
}

// close closes the current session
func (s *Session) close() error {
	if s.sigintHandlerQuitChan != nil {
		close(s.sigintHandlerQuitChan)
	}

	err := s.CloseStdin()
	if err != nil {
		return fmt.Errorf("failed to close stdin: %s", err)
	}

	err = s.sshSess.Close()
	if err != nil {
		return fmt.Errorf("failed to close session: %s", err)
	}

	return nil
}

// newSession creates a new session
func newSession(ctx context.Context, client *ssh.Client, withTerminal bool) (*Session, error) {
	sshSess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to initialise session: %s", err)
	}

	stdin, err := sshSess.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get the session stdin pipe: %s", err)
	}

	// Request a pseudo terminal for forwarding signals to the remote process
	// NB: scp misbehaves when a pseudoterminal is attached, but we need one
	// to cancel other commands...
	if withTerminal {
		err = sshSess.RequestPty("xterm", 80, 40,
			ssh.TerminalModes{
				ssh.ECHO:          0,     // disable echoing
				ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
				ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to setup the pseudo terminal: %s", err)
		}
	}

	// If requested, send SIGINT to the remote process and close the session
	quitChan := make(chan struct{})
	sess := Session{sshSess: sshSess, stdin: stdin, sigintHandlerQuitChan: quitChan}
	go func() {
		select {
		case <-ctx.Done():
			if withTerminal {
				// Looks like ssh.SIGINT is not supported by OpenSSH. What a bummer :(
				// https://github.com/golang/go/issues/4115#issuecomment-66070418
				// https://github.com/golang/go/issues/16597
				// err := sshSess.Signal(ssh.SIGINT)
				_, err := stdin.Write([]byte("\x03"))
				if err != nil && err != io.EOF {
					log.Warnf("Failed to send SIGINT to the remote process: %s", err)
				}
			}
			err := sess.CloseStdin()
			if err != nil {
				log.Warnf("Failed to close session stdin: %s", err)
			}
			err = ctx.Err()
			if err == context.DeadlineExceeded {
				log.Warnf("Context deadline exceeeded on server: %s", client.RemoteAddr())
			}
		case <-quitChan:
			// Stop the signal handler when the task completes
		}
	}()

	return &sess, nil
}

type ExecCallbackFunc func(*Session) (error, *errgroup.Group)

type Connection interface {
	Close() error
	Exec(context.Context, bool, ExecCallbackFunc) error
	GetAddress() string
	GetHost() string
	// TODO: Redesign this to avoid mutating state
	SetError(error)
}

type connection struct {
	Server *inventory.Server
	client *ssh.Client
}

func (conn *connection) Close() error {
	return conn.client.Close()
}

func (conn *connection) Exec(ctx context.Context, withTerminal bool, fn ExecCallbackFunc) error {
	sess, err := newSession(ctx, conn.client, withTerminal)
	if err != nil {
		return fmt.Errorf("failed to create new session: %s", err)
	}
	// TODO: Log error
	defer sess.close()

	err, errGroup := fn(sess)
	if err != nil {
		return fmt.Errorf("failed to start the ssh command: %s", err)
	}

	// Wait for the session to finish running
	err = sess.wait()
	if err != nil {
		// Check the async operation (if there is any) for the error
		// cause before returning
		err = fmt.Errorf("failed ssh command: %s", err)
	}

	if errGroup != nil {
		asyncErr := errGroup.Wait()
		if asyncErr != nil {
			err = fmt.Errorf("%s: failed async ssh operation: %s", err, asyncErr)
		}
	}

	if err != nil {
		return err
	}

	// Make sure we always return some error when the command is cancelled
	return ctx.Err()
}

func (conn *connection) GetAddress() string {
	return conn.Server.GetAddress()
}

func (conn *connection) GetHost() string {
	return conn.Server.Host
}

func (conn *connection) SetError(err error) {
	conn.Server.SetError(err)
}

func NewConnection(server *inventory.Server, timeout time.Duration) (Connection, error) {
	sshConfig := &ssh.ClientConfig{
		User: server.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(server.Password),
		},
		Timeout: timeout,
		// TODO: Be stricter about security
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", server.GetAddress(), sshConfig)
	if err != nil {
		return nil, fmt.Errorf("dial error: %s", err)
	}

	return &connection{Server: server, client: client}, nil
}

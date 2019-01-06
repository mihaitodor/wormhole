package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	yaml "gopkg.in/yaml.v2"
)

type Config struct {
	Playbook                 string
	PlaybookFolder           string
	Inventory                string
	ConnectTimeout           time.Duration
	ExecTimeout              time.Duration
	MaxConcurrentConnections int
}

type Server struct {
	Host        string
	Port        uint
	Username    string
	Password    string
	playbookErr error
}

func (s *Server) GetAddress() string {
	address := s.Host + ":22"
	if s.Port != 0 {
		address = fmt.Sprintf("%s:%d", s.Host, s.Port)
	}
	return address
}

type Connection struct {
	Server *Server
	Client *ssh.Client
}

type Inventory []*Server

func (i Inventory) getAllServers(predFn func(*Server) bool) []string {
	var servers []string
	for _, s := range i {
		if predFn != nil {
			if predFn(s) {
				servers = append(servers, s.GetAddress())
			}
		} else {
			servers = append(servers, s.GetAddress())
		}
	}

	return servers
}

func (i Inventory) getAllCompletedServers() []string {
	return i.getAllServers(func(s *Server) bool {
		return s.playbookErr == nil
	})
}

func (i Inventory) getAllFailedServers() []string {
	return i.getAllServers(func(s *Server) bool {
		return s.playbookErr != nil
	})
}

func LoadInventory(inventoryFile string) (Inventory, error) {
	fileContents, err := ioutil.ReadFile(inventoryFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open inventory file: %s", err)
	}

	var inventory Inventory
	err = yaml.Unmarshal(fileContents, &inventory)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal inventory contents: %s", err)
	}

	return inventory, nil
}

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
		case <-quitChan:
			// Stop the signal handler when the task completes
		}
	}()

	return &sess, nil
}

func SshExec(ctx context.Context, client *ssh.Client, withTerminal bool, fn func(*Session) (error, *errgroup.Group)) error {
	sess, err := newSession(ctx, client, withTerminal)
	if err != nil {
		fmt.Errorf("failed to create new session: %s", err)
	}
	// TODO: Log error
	defer sess.close()

	prepErr, g := fn(sess)
	if prepErr != nil {
		return fmt.Errorf("failed to start the ssh command: %s", err)
	}

	// Wait for the session to finish running
	err = sess.wait()
	if err != nil {
		// Check the async operation (if there is any) for the error
		// cause before returning it
		err = fmt.Errorf("failed ssh command: %s", err)
	}

	if g != nil {
		asyncErr := g.Wait()
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

type Action interface {
	GetType() string
	Run(context.Context, *ssh.Client, Config) error
}

type FileAction struct {
	Src   string
	Dest  string
	Owner string
	Group string
	Mode  string
}

func (*FileAction) GetType() string {
	return "file"
}

// Copies the contents of src to dest on a remote host
func copyFile(sess *Session, timeout time.Duration, src io.Reader, size int64, dest, mode string) error {
	// Instruct the remote scp process that we want to bail out immediately
	// TODO: Log error
	defer sess.CloseStdin()

	_, err := fmt.Fprintln(sess.Stdin(), "C"+mode, size, filepath.Base(dest))
	if err != nil {
		return fmt.Errorf("failed to create remote file: %s", err)
	}

	_, err = io.Copy(sess.Stdin(), src)
	if err != nil {
		return fmt.Errorf("failed to write remote file contents: %s", err)
	}

	_, err = fmt.Fprint(sess.Stdin(), "\x00")
	if err != nil {
		return fmt.Errorf("failed to close remote file: %s", err)
	}

	return nil
}

func (a *FileAction) Run(ctx context.Context, client *ssh.Client, config Config) error {
	err := SshExec(ctx, client, false, func(sess *Session) (error, *errgroup.Group) {
		f, err := os.Open(filepath.Join(config.PlaybookFolder, a.Src))
		if err != nil {
			return fmt.Errorf("failed to open source file: %s", err), nil
		}
		stat, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to get source file info: %s", err), nil
		}

		// Start scp receiver on the remote host
		err = sess.Start("scp -qt " + filepath.Dir(a.Dest))
		if err != nil {
			return fmt.Errorf("failed to start scp receiver: %s", err), nil
		}

		mode := a.Mode
		if mode == "" {
			mode = "0644"
		}

		var g errgroup.Group
		g.Go(func() error {
			return copyFile(
				sess,
				config.ExecTimeout,
				f,
				stat.Size(),
				a.Dest,
				mode,
			)
		})
		return nil, &g
	})
	if err != nil {
		return fmt.Errorf("failed to copy file %q: %s", a.Src, err)
	}

	if a.Owner != "" && a.Group != "" {
		err = SshExec(ctx, client, true, func(sess *Session) (error, *errgroup.Group) {
			return sess.Start(
				fmt.Sprintf("chown %s:%s %s", a.Owner, a.Group, a.Dest),
			), nil
		})
		if err != nil {
			return fmt.Errorf(
				"failed to set the file owner on %q to %s:%s: %s",
				a.Dest, a.Owner, a.Group, err,
			)
		}
	}

	return nil
}

type AptAction struct {
	State string
	Pkg   []string
}

func (*AptAction) GetType() string {
	return "apt"
}

func (a *AptAction) Run(ctx context.Context, client *ssh.Client, config Config) error {
	// Update package lists first
	err := SshExec(ctx, client, true, func(sess *Session) (error, *errgroup.Group) {
		return sess.Start("apt-get update"), nil
	})
	if err != nil {
		return fmt.Errorf("failed to update package lists: %s", err)
	}

	// Install the requested packages
	for _, pkg := range a.Pkg {
		err = SshExec(ctx, client, true, func(sess *Session) (error, *errgroup.Group) {
			return sess.Start(fmt.Sprintf("apt-get install -y %s", pkg)), nil
		})
		if err != nil {
			return fmt.Errorf("failed to install package %q: %s", pkg, err)
		}
	}

	return nil
}

type ServiceAction struct {
	Name  string
	State string
}

func (*ServiceAction) GetType() string {
	return "service"
}

func (a *ServiceAction) Run(ctx context.Context, client *ssh.Client, config Config) error {
	return SshExec(ctx, client, true, func(sess *Session) (error, *errgroup.Group) {
		return sess.Start(fmt.Sprintf("service %s %s", a.Name, a.State)), nil
	})
}

type ShellAction string

func (*ShellAction) GetType() string {
	return "shell"
}

func (a *ShellAction) Run(ctx context.Context, client *ssh.Client, config Config) error {
	return SshExec(ctx, client, true, func(sess *Session) (error, *errgroup.Group) {
		return sess.Start(string(*a)), nil
	})
}

type Task struct {
	Name    string
	Actions []Action
}

type Playbook struct {
	Name  string
	Tasks []*Task
}

func getActionByName(name string) (Action, error) {
	switch name {
	case "file":
		return new(FileAction), nil
	case "apt":
		return new(AptAction), nil
	case "service":
		return new(ServiceAction), nil
	case "shell":
		return new(ShellAction), nil
	default:
		return nil, fmt.Errorf("unrecognised action %q", name)
	}
}

// UnmarshalYAML unmarshals a task and populates known actions into their
// specific objects using mapstructure. This is not a very efficient
// way of doing it, due to the double parsing.
func (t *Task) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var rawTask map[string]interface{}
	err := unmarshal(&rawTask)
	if err != nil {
		return fmt.Errorf("failed to unmarshal task: %s", err)
	}

	rawName, ok := rawTask["name"]
	if !ok {
		return errors.New("missing 'name' field")
	}

	name, ok := rawName.(string)
	if !ok || name == "" {
		return errors.New("'name' field needs to be a non-empty string")
	}

	t.Name = name

	// Delete name item, since it doesn't represent an action
	delete(rawTask, "name")

	for actionName, rawAction := range rawTask {
		action, err := getActionByName(actionName)
		if err != nil {
			return fmt.Errorf("failed to instantiate action %q: %s", actionName, err)
		}

		err = mapstructure.Decode(rawAction, action)
		if err != nil {
			return fmt.Errorf("failed to decode action %q: %s", actionName, err)
		}

		t.Actions = append(t.Actions, action)
	}

	return nil
}

func LoadPlaybook(playbookFile string) (*Playbook, error) {
	fileContents, err := ioutil.ReadFile(playbookFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open playbook file: %s", err)
	}

	var playbook Playbook
	err = yaml.Unmarshal(fileContents, &playbook.Tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal playbook contents: %s", err)
	}

	return &playbook, nil
}

func RunPlaybook(ctx context.Context, wg *sync.WaitGroup, conn *Connection, config Config, playbook *Playbook) {
	defer wg.Done()

	for idx, task := range playbook.Tasks {
		log.Infof(
			"Runing task [%d/%d] on %q: %s", idx+1,
			len(playbook.Tasks), conn.Server.GetAddress(), task.Name,
		)

		for _, a := range task.Actions {
			err := a.Run(ctx, conn.Client, config)
			if err != nil {
				// Something went wrong and the playbook needs to be
				// rerun on this host.
				log.Warnf(
					"Failed to run action %q on %q: %s",
					a.GetType(), conn.Server.GetAddress(), err,
				)

				conn.Server.playbookErr = err

				return
			}
		}
	}
}

func connectServer(server *Server, timeout time.Duration) (*Connection, error) {
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

	return &Connection{Server: server, Client: client}, nil
}

func Run(ctx context.Context, config Config, playbook *Playbook, inventory Inventory) {
	for start := 0; start < len(inventory); start += config.MaxConcurrentConnections {
		end := start + config.MaxConcurrentConnections
		if end > len(inventory) {
			end = len(inventory)
		}

		log.Infof("Running playbook on servers: %s",
			strings.Join(inventory[start:end].getAllServers(nil), ", "),
		)

		// Open a ssh session to each server in the current batch
		var connections []*Connection
		for _, server := range inventory[start:end] {
			client, err := connectServer(server, config.ConnectTimeout)
			if err != nil {
				err = fmt.Errorf("Failed to connect to server %q: %s", server.GetAddress(), err)
				server.playbookErr = err
				log.Warn(err)
				continue
			}

			connections = append(connections, client)
		}

		if len(connections) == 0 {
			continue
		}

		var wg sync.WaitGroup
		wg.Add(len(connections))
		for _, conn := range connections {
			go RunPlaybook(ctx, &wg, conn, config, playbook)
		}
		wg.Wait()

		// Close ssh clients
		for _, conn := range connections {
			err := conn.Client.Close()
			if err != nil {
				log.Warnf(
					"Failed to close ssh connection to server %q: %s",
					conn.Server.GetAddress(), err,
				)
			}
		}
	}
}

func InitConfing() Config {
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

func InitGracefulStop() context.Context {
	gracefulStop := make(chan os.Signal)
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
	config := InitConfing()

	inventory, err := LoadInventory(config.Inventory)
	if err != nil {
		log.Fatalf("Failed to load inventory: %s", err)
	}

	playbook, err := LoadPlaybook(config.Playbook)
	if err != nil {
		log.Fatalf("Failed to load playbook: %s", err)
	}

	ctx := InitGracefulStop()

	Run(ctx, config, playbook, inventory)

	completed := inventory.getAllCompletedServers()
	if len(completed) > 0 {
		log.Infof("Playbook ran successfully on servers: %s", strings.Join(completed, ", "))
	}

	failed := inventory.getAllFailedServers()
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
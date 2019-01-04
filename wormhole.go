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
	"sync"
	"syscall"
	"time"

	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
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
	Address  string
	Username string
	Password string
}

type Connection struct {
	Server Server
	Client *ssh.Client
}

type Inventory []Server

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

func SshExec(ctx context.Context, client *ssh.Client, fn func(*ssh.Session) error) error {
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to create session: %s", err)
	}
	defer sess.Close()

	remoteStdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get the session stdin pipe: %s", err)
	}
	defer remoteStdin.Close()

	// Request a pseudo terminal for forwarding signals to the remote process
	err = sess.RequestPty("xterm", 80, 40,
		ssh.TerminalModes{
			ssh.ECHO:          0,     // disable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		},
	)
	if err != nil {
		return fmt.Errorf("failed to setup the pseudo terminal: %s", err)
	}

	// If requested, send SIGINT to the remote process and close the session
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		select {
		case <-ctx.Done():
			// Looks like ssh.SIGINT is not supported by OpenSSH. What a bummer :(
			// https://github.com/golang/go/issues/4115#issuecomment-66070418
			// https://github.com/golang/go/issues/16597
			// err := sess.Signal(ssh.SIGINT)
			_, err := remoteStdin.Write([]byte("\x03"))
			if err != nil && err != io.EOF {
				log.Warnf("Failed to send SIGINT to the remote process: %s", err)
			}
		case <-quit:
			// Let this goroutine exit
		}
	}()

	err = fn(sess)
	if err != nil {
		return fmt.Errorf("failed to start the ssh command: %s", err)
	}

	// Wait for the ssh command to finish running
	err = sess.Wait()
	if err != nil {
		return fmt.Errorf("ssh command failed: %s", err)
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

// waitTimeout waits for the waitgroup for the specified max timeout.
// Returns true if waiting timed out.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

// Copies the contents of src to dest on a remote host
func copyFile(sess *ssh.Session, timeout time.Duration, src, dest, mode string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %s", err)
	}
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info: %s", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	errCh := make(chan error, 2)

	w, err := sess.StdinPipe()
	defer w.Close()

	// Start the scp receiver on the remote host
	err = sess.Start("/usr/bin/scp -qt " + filepath.Dir(dest))
	if err != nil {
		return err
	}

	go func() {
		defer wg.Done()

		_, err = fmt.Fprintln(w, "C"+mode, stat.Size(), filepath.Base(dest))
		if err != nil {
			errCh <- err
			return
		}

		_, err = io.Copy(w, f)
		if err != nil {
			errCh <- err
			return
		}

		_, err = fmt.Fprint(w, "\x00")
		if err != nil {
			errCh <- err
			return
		}
	}()

	if waitTimeout(&wg, timeout) {
		return errors.New("timeout when upload files")
	}
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *FileAction) Run(ctx context.Context, client *ssh.Client, config Config) error {
	err := SshExec(ctx, client, func(sess *ssh.Session) error {

		mode := a.Mode
		if mode == "" {
			mode = "0644"
		}

		return copyFile(
			sess,
			config.ExecTimeout,
			filepath.Join(config.PlaybookFolder, a.Src),
			a.Dest,
			mode,
		)
	})
	if err != nil {
		return fmt.Errorf("failed to copy file %q: %s", a.Src, err)
	}

	if a.Owner != "" && a.Group != "" {
		err = SshExec(ctx, client, func(sess *ssh.Session) error {
			return sess.Start(
				fmt.Sprintf("chown %s:%s %s", a.Owner, a.Group, a.Dest),
			)
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
	err := SshExec(ctx, client, func(sess *ssh.Session) error {
		return sess.Start("apt-get update")
	})
	if err != nil {
		return fmt.Errorf("failed to update package lists: %s", err)
	}

	// Install the requested packages
	for _, pkg := range a.Pkg {
		err = SshExec(ctx, client, func(sess *ssh.Session) error {
			return sess.Start(fmt.Sprintf("apt-get install -y %s", pkg))
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
	return SshExec(ctx, client, func(sess *ssh.Session) error {
		return sess.Start(fmt.Sprintf("service %s %s", a.Name, a.State))
	})
}

type ShellAction string

func (*ShellAction) GetType() string {
	return "shell"
}

func (a *ShellAction) Run(ctx context.Context, client *ssh.Client, config Config) error {
	return SshExec(ctx, client, func(sess *ssh.Session) error {
		return sess.Start(string(*a))
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
			len(playbook.Tasks), conn.Server.Address, task.Name,
		)

		for _, a := range task.Actions {
			err := a.Run(ctx, conn.Client, config)
			if err != nil {
				// Something went wrong. The whole playbook
				// needs to be rerun on this host.
				log.Warnf(
					"Failed to run action %q on %q: %s",
					a.GetType(), conn.Server.Address, err,
				)
				return
			}
		}
	}
}

func connectServer(server Server, timeout time.Duration) (*Connection, error) {
	sshConfig := &ssh.ClientConfig{
		User: server.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(server.Password),
		},
		Timeout: timeout,
		// TODO: Be stricter about security
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", server.Address, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("dial error: %s", err)
	}

	return &Connection{Server: server, Client: client}, nil
}

func Run(ctx context.Context, config Config, playbook *Playbook, inventory Inventory) error {
	for start := 0; start < len(inventory); start += config.MaxConcurrentConnections {
		end := start + config.MaxConcurrentConnections
		if end > len(inventory) {
			end = len(inventory)
		}

		// Open a ssh session to each server in the current batch
		var connections []*Connection
		for _, server := range inventory[start:end] {
			client, err := connectServer(server, config.ConnectTimeout)
			if err != nil {
				log.Warnf("Failed to connect to server %q: %s", server.Address, err)
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
					"Failed to close connection to server %q: %s",
					conn.Server.Address, err,
				)
			}
		}
	}

	return nil
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

	err = Run(ctx, config, playbook, inventory)
	if err != nil {
		log.Fatalf("Failed to run playbook: %s", err)
	}

	select {
	case <-ctx.Done():
		log.Fatalf("Abnormal termination due to: %s", ctx.Err())
	default:
		// Exit normally
	}
}

package playbook

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/mihaitodor/wormhole/actions"
	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/connection"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

type Task struct {
	Name    string
	Actions []actions.Action
}

type Playbook struct {
	Name  string
	Tasks []*Task
}

func (p *Playbook) Run(ctx context.Context, wg *sync.WaitGroup, conn *connection.Connection, conf config.Config) {
	defer wg.Done()

	for idx, task := range p.Tasks {
		log.Infof(
			"Runing task [%d/%d] on %q: %s", idx+1,
			len(p.Tasks), conn.Server.GetAddress(), task.Name,
		)

		for _, a := range task.Actions {
			// Make sure we cancel the action if ExecTimeout is exceeded
			ctx, cancel := context.WithTimeout(ctx, conf.ExecTimeout)
			err := a.Run(ctx, conn, conf)
			cancel()
			if err != nil {
				// Something went wrong and the playbook needs to be
				// rerun on this host.
				log.Warnf(
					"Failed to run action %q on %q: %s",
					a.GetType(), conn.Server.GetAddress(), err,
				)

				conn.Server.SetError(err)

				return
			}
		}
	}
}

func NewPlaybook(playbookFile string) (*Playbook, error) {
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

type ActionContainer interface {
	GetAction() actions.Action
	IsNil() bool
}

type FileActionContainer struct {
	Action *actions.FileAction `yaml:"file"`
}

func (ac *FileActionContainer) GetAction() actions.Action {
	return ac.Action
}

func (ac *FileActionContainer) IsNil() bool {
	return ac.Action == nil
}

type AptActionContainer struct {
	Action *actions.AptAction `yaml:"apt"`
}

func (ac *AptActionContainer) GetAction() actions.Action {
	return ac.Action
}

func (ac *AptActionContainer) IsNil() bool {
	return ac.Action == nil
}

type ServiceActionContainer struct {
	Action *actions.ServiceAction `yaml:"service"`
}

func (ac *ServiceActionContainer) GetAction() actions.Action {
	return ac.Action
}

func (ac *ServiceActionContainer) IsNil() bool {
	return ac.Action == nil
}

type ShellActionContainer struct {
	Action *actions.ShellAction `yaml:"shell"`
}

func (ac *ShellActionContainer) GetAction() actions.Action {
	return ac.Action
}

func (ac *ShellActionContainer) IsNil() bool {
	return ac.Action == nil
}

type ValidateActionContainer struct {
	Action *actions.ValidateAction `yaml:"validate"`
}

func (ac *ValidateActionContainer) GetAction() actions.Action {
	return ac.Action
}

func (ac *ValidateActionContainer) IsNil() bool {
	return ac.Action == nil
}

// getActionContainerByName returns a struct containing a pointer
// to a specific action to be unmarshaled by the YAML parser
// TODO: Find a way to implement this without redundant methods
// on the action containers
func unmarshalAction(name string, unmarshal func(interface{}) error) (ActionContainer, error) {
	var ac ActionContainer
	switch name {
	case "file":
		ac = &FileActionContainer{}
	case "apt":
		ac = &AptActionContainer{}
	case "service":
		ac = &ServiceActionContainer{}
	case "shell":
		ac = &ShellActionContainer{}
	case "validate":
		ac = &ValidateActionContainer{}
	default:
		return nil, errors.New("unrecognised action")
	}

	err := unmarshal(ac)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal action: %s", err)
	}

	if ac.IsNil() {
		return nil, errors.New("empty action")
	}

	ac.GetAction().SetType(name)

	return ac, nil
}

// UnmarshalYAML unmarshals a task and populates known actions into their
// specific objects.
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

	taskName, ok := rawName.(string)
	if !ok || taskName == "" {
		return errors.New("'name' field needs to be a non-empty string")
	}

	t.Name = taskName

	// Delete name item, since it doesn't represent an action
	delete(rawTask, "name")

	for actionName := range rawTask {
		actionContainer, err := unmarshalAction(actionName, unmarshal)
		if err != nil {
			return fmt.Errorf("failed to unmarshal action %q from task %q: %s", actionName, t.Name, err)
		}

		t.Actions = append(t.Actions, actionContainer.GetAction())
	}

	return nil
}

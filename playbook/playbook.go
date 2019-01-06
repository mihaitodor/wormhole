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
			err := a.Run(ctx, conn, conf)
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

func (f *FileActionContainer) GetAction() actions.Action {
	return f.Action
}

func (f *FileActionContainer) IsNil() bool {
	return f.Action == nil
}

type AptActionContainer struct {
	Action *actions.AptAction `yaml:"apt"`
}

func (f *AptActionContainer) GetAction() actions.Action {
	return f.Action
}

func (f *AptActionContainer) IsNil() bool {
	return f.Action == nil
}

type ServiceActionContainer struct {
	Action *actions.ServiceAction `yaml:"service"`
}

func (f *ServiceActionContainer) GetAction() actions.Action {
	return f.Action
}

func (f *ServiceActionContainer) IsNil() bool {
	return f.Action == nil
}

type ShellActionContainer struct {
	Action *actions.ShellAction `yaml:"shell"`
}

func (f *ShellActionContainer) GetAction() actions.Action {
	return f.Action
}

func (f *ShellActionContainer) IsNil() bool {
	return f.Action == nil
}

type ValidateActionContainer struct {
	Action *actions.ValidateAction `yaml:"validate"`
}

func (f *ValidateActionContainer) GetAction() actions.Action {
	return f.Action
}

func (f *ValidateActionContainer) IsNil() bool {
	return f.Action == nil
}

// getActionContainerByName returns a struct containing a pointer
// to a specific action to be unmarshaled by the YAML parser
// TODO: Find a way to implement this without redundant methods
// on the action containers
func getActionContainerByName(name string) (ActionContainer, error) {
	switch name {
	case "file":
		return &FileActionContainer{}, nil
	case "apt":
		return &AptActionContainer{}, nil
	case "service":
		return &ServiceActionContainer{}, nil
	case "shell":
		return &ShellActionContainer{}, nil
	case "validate":
		return &ValidateActionContainer{}, nil
	default:
		return nil, fmt.Errorf("unrecognised action %q", name)
	}
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
		actionContainer, err := getActionContainerByName(actionName)
		if err != nil {
			return fmt.Errorf("failed to instantiate action %q: %s", actionName, err)
		}

		err = unmarshal(actionContainer)
		if err != nil {
			return fmt.Errorf("failed to unmarshal task: %s", err)
		}

		if actionContainer.IsNil() {
			return fmt.Errorf("task %q contains empty action %q", t.Name, actionName)
		}

		t.Actions = append(t.Actions, actionContainer.GetAction())
	}

	return nil
}

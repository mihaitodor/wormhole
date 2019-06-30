package playbook

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/mihaitodor/wormhole/actions"
	"github.com/mihaitodor/wormhole/config"
	"github.com/mihaitodor/wormhole/transport"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

type Task struct {
	Name    string
	Actions []actions.Action
}

type Playbook struct {
	Tasks []Task
}

func (p *Playbook) Run(ctx context.Context, wg *sync.WaitGroup, conn transport.Connection, conf config.Config) {
	defer wg.Done()

	for idx, task := range p.Tasks {
		log.Infof(
			"Running task [%d/%d] on %q: %s", idx+1,
			len(p.Tasks), conn.GetAddress(), task.Name,
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
					a.GetType(), conn.GetAddress(), err,
				)

				conn.SetError(err)

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

	// Delete name field, since it doesn't represent an action
	delete(rawTask, "name")

	for actionType, action := range rawTask {
		action, err := actions.UnmarshalAction(actionType, action)
		if err != nil {
			return fmt.Errorf("failed to unmarshal action %q from task %q: %s", actionType, t.Name, err)
		}

		t.Actions = append(t.Actions, action)
	}

	if len(t.Actions) == 0 {
		return fmt.Errorf("task %q has no actions", t.Name)
	}

	return nil
}

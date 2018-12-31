package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/mitchellh/mapstructure"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	yaml "gopkg.in/yaml.v2"
)

// Move this stuff to a function...
var (
	verbose = kingpin.Flag("verbose", "Verbose mode.").Short('v').Bool()
	name    = kingpin.Arg("name", "Name of user.").Required().String()
)

type Config struct {
	PlaybooksFolder   string
	ConnectTimeout    time.Duration
	ProcessingTimeout time.Duration
	ConcurrentActions int
}

type Server struct {
	Address  string
	Username string
	Password string
}

type Inventory []Server

func LoadInventory(inventoryFile string) (*Inventory, error) {
	fileContents, err := ioutil.ReadFile(inventoryFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open inventory file: %s", err)
	}

	var inventory Inventory
	err = yaml.Unmarshal(fileContents, &inventory)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal inventory contents: %s", err)
	}

	return &inventory, nil
}

// TODO: See if this is still needed further along
// const (
// 	ActionFile = iota
// 	// ActionTemplate
// 	ActionApt
// )

// type ActionType int

// var (
// 	knownActions = map[string]struct{}{
// 		"file":    {},
// 		"apt":     {},
// 		"service": {},
// 	}
// )

type FileAction struct {
	Src   string
	Dest  string
	Owner string
	Group string
	Mode  string
}

type AptAction struct {
	State string
	Pkg   []string
}

type ServiceAction struct {
	Name  string
	State string
}

type Task struct {
	Name    string
	Actions []interface{}
}

type Playbook struct {
	Name  string
	Tasks []*Task
}

func getActionByName(name string) (interface{}, error) {
	switch name {
	case "file":
		return &FileAction{}, nil
	case "apt":
		return &AptAction{}, nil
	case "service":
		return &ServiceAction{}, nil
	}

	return nil, fmt.Errorf("unrecognised action %q", name)
}

// Implements the Unmarshaler interface of the yaml pkg.
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

	// delete name item, since it doesn't represent an action
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

func RunPlaybook(playbook *Playbook, inventory *Inventory) error {
	for _, task := range playbook.Tasks {
		log.Printf("Runing task: %s", task.Name)

		// for _, server := range *inventory {

		// }
	}

	return nil
}

func main() {
	/*
		do Config via Kingpin!
		inventoryFile: inventory.yaml
		playbooksFolder: playbooks
		connectTimeout: 30s
		processingTimeout: 1m
		concurrentActions: 2
	*/

	inventory, err := LoadInventory("inventory.yaml")
	if err != nil {
		log.Fatalf("Failed to load inventory: %s", err)
	}

	spew.Dump(inventory)

	playbook, err := LoadPlaybook("playbooks/wormhole.yaml")
	if err != nil {
		log.Fatalf("Failed to load playbook: %s", err)
	}

	// spew.Dump(playbook)

	err = RunPlaybook(playbook, inventory)
}

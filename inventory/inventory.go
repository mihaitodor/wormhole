package inventory

import (
	"fmt"
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

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

func (s *Server) SetError(err error) {
	s.playbookErr = err
}

func (s *Server) GetError() error {
	return s.playbookErr
}

type Inventory []*Server

func (i Inventory) GetAllServers(predFn func(*Server) bool) []string {
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

func (i Inventory) GetAllCompletedServers() []string {
	return i.GetAllServers(func(s *Server) bool {
		return s.playbookErr == nil
	})
}

func (i Inventory) GetAllFailedServers() []string {
	return i.GetAllServers(func(s *Server) bool {
		return s.playbookErr != nil
	})
}

func NewInventory(inventoryFile string) (Inventory, error) {
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

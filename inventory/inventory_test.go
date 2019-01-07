package inventory

import (
	"errors"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func Test_Inventory(t *testing.T) {
	server1 := &Server{Host: "gondor"}
	server2 := &Server{Host: "mordor", Port: 2222}
	i := Inventory{server1, server2}

	Convey("Inventory.GetAllServers()", t, func() {
		Convey("should return all servers", func() {
			servers := i.GetAllServers(nil)
			So(servers, ShouldHaveLength, 2)

			Convey("with ports attached", func() {
				So(servers[0], ShouldEqual, server1.Host+":22")
				So(servers[1], ShouldEqual, fmt.Sprintf("%s:%d", server2.Host, server2.Port))
			})
		})

		Convey("should return all servers even when one of them has an error", func() {
			server1.SetError(errors.New("hobbits not found"))
			servers := i.GetAllServers(nil)
			So(servers, ShouldHaveLength, 2)
		})

		Convey("should return all servers matching custom predicate", func() {
			servers := i.GetAllServers(func(s *Server) bool {
				return s.Host != server2.Host
			})
			So(servers, ShouldHaveLength, 1)
			So(servers[0], ShouldContainSubstring, server1.Host)
		})
	})

	Convey("Inventory.GetAllCompletedServers()", t, func() {
		server1.SetError(errors.New("hobbits not found"))
		servers := i.GetAllCompletedServers()
		Convey("should return all servers without errors", func() {
			So(servers, ShouldHaveLength, 1)
			So(servers[0], ShouldContainSubstring, server2.Host)
		})
	})

	Convey("Inventory.GetAllFailedServers()", t, func() {
		server1.SetError(errors.New("hobbits not found"))
		servers := i.GetAllFailedServers()
		Convey("should return all servers without errors", func() {
			So(servers, ShouldHaveLength, 1)
			So(servers[0], ShouldContainSubstring, server1.Host)
		})
	})
}

func Test_NewInventory(t *testing.T) {
	Convey("NewInventory()", t, func() {
		i, err := NewInventory("fixtures/inventory.yaml")

		Convey("should run successfully", func() {
			So(err, ShouldBeNil)
			So(i, ShouldHaveLength, 2)
			So(i[0].Host, ShouldEqual, "gondor")
			So(i[0].Username, ShouldEqual, "isildur")
			So(i[1].Port, ShouldEqual, 4444)
			So(i[1].Password, ShouldEqual, "thou shalt not pass")
		})
	})
}

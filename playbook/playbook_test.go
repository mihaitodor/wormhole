package playbook

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func Test_NewPlaybook(t *testing.T) {
	Convey("Inventory.NewPlaybook()", t, func() {
		Convey("should load a playbook", func() {
			p, err := NewPlaybook("fixtures/playbook.yaml")

			So(err, ShouldBeNil)
			So(p.Tasks, ShouldHaveLength, 2)
			So(p.Tasks[0].Actions[0].GetType(), ShouldEqual, "apt")
			So(p.Tasks[1].Name, ShouldEqual, "Test file and shell actions")
			So(p.Tasks[1].Actions, ShouldHaveLength, 2)
		})

		Convey("should reject playbooks with empty tasks", func() {
			_, err := NewPlaybook("fixtures/playbook_task_no_actions.yaml")

			So(err.Error(), ShouldContainSubstring, "has no actions")
		})

		Convey("should reject playbooks with nameless tasks", func() {
			_, err := NewPlaybook("fixtures/playbook_task_no_name.yaml")

			So(err.Error(), ShouldContainSubstring, "'name' field needs to be a non-empty string")
		})

		Convey("should reject playbooks with unrecognised actions", func() {
			_, err := NewPlaybook("fixtures/playbook_task_unrecognised_action.yaml")

			So(err.Error(), ShouldContainSubstring, "unrecognised action")
		})

		Convey("should reject playbooks with invalid actions", func() {
			_, err := NewPlaybook("fixtures/playbook_task_invalid_action.yaml")

			So(err.Error(), ShouldContainSubstring, "failed to unmarshall 'shell' action")
		})
	})
}

package actions

import (
	"fmt"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func Test_UnmarshalAction(t *testing.T) {
	Convey("UnmarshalAction()", t, func() {
		Convey("should decode an apt action", func() {
			actionType := "apt"
			state := "installed"
			packages := []string{"apache", "php"}
			aptAction := map[string]interface{}{
				"state": state,
				"pkg":   packages,
			}

			action, err := UnmarshalAction(actionType, aptAction)
			So(err, ShouldBeNil)
			So(action.GetType(), ShouldEqual, actionType)
			So(action, ShouldHaveSameTypeAs, &AptAction{})
			So(action.(*AptAction).State, ShouldEqual, state)
			So(action.(*AptAction).Pkg, ShouldResemble, packages)
		})

		Convey("should decode actions which are represented as `key: value`", func() {
			actionType := "shell"
			shellAction := "echo kaboom"

			action, err := UnmarshalAction(actionType, shellAction)
			So(err, ShouldBeNil)
			So(action.GetType(), ShouldEqual, actionType)
			So(action, ShouldHaveSameTypeAs, &ShellAction{})
			So(action.(*ShellAction).Command, ShouldEqual, shellAction)
		})

		Convey("should decode actions which have duration fields", func() {
			actionType := "validate"
			timeout := 5 * time.Second
			aptAction := map[string]interface{}{
				"timeout": timeout.String(),
			}

			action, err := UnmarshalAction(actionType, aptAction)
			So(err, ShouldBeNil)
			So(action.GetType(), ShouldEqual, actionType)
			So(action, ShouldHaveSameTypeAs, &ValidateAction{})
			So(action.(*ValidateAction).Timeout, ShouldEqual, timeout)
		})

		Convey("should fail to decode unrecognised actions", func() {
			actionType := "foobar"

			_, err := UnmarshalAction(actionType, struct{}{})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, fmt.Sprintf("unrecognised action: %s", actionType))
		})

		Convey("should fail to decode actions with unrecognised fields", func() {
			dummyKey := "foo"
			_, err := UnmarshalAction("apt", map[string]string{
				dummyKey: "bar",
			})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, fmt.Sprintf("has invalid keys: %s", dummyKey))
		})
	})
}

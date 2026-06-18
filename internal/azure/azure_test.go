package azure

import "testing"

func TestInfoRunning(t *testing.T) {
	running := Info{Exists: true, PowerState: "VM running"}
	if !running.Running() {
		t.Error(`"VM running" should be Running()`)
	}
	deallocated := Info{Exists: true, PowerState: "VM deallocated"}
	if deallocated.Running() {
		t.Error(`"VM deallocated" should not be Running()`)
	}
	stopped := Info{Exists: true, PowerState: "VM stopped"}
	if stopped.Running() {
		t.Error(`"VM stopped" should not be Running()`)
	}
	absent := Info{Exists: false, PowerState: ""}
	if absent.Running() {
		t.Error("non-existent VM should not be Running()")
	}
}

package telemetry

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInitWithoutLicenseReturnsNoop(t *testing.T) {
	t.Setenv(envLicenseKey, "")
	t.Setenv(envAppName, "")
	sink, cleanup := Init(Config{})
	defer cleanup()
	if _, ok := sink.(NoopSink); !ok {
		t.Fatalf("sink = %T, want NoopSink", sink)
	}
	sink.RecordUp(UpEvent{})
	sink.RecordProvision(ProvisionEvent{})
	sink.RecordDiagnostic(DiagnosticEvent{})
}

func TestInitWithLicenseConstructsAgent(t *testing.T) {
	t.Setenv(envLicenseKey, strings.Repeat("1", 40))
	t.Setenv(envAppName, "Rover Test")
	sink, cleanup := Init(Config{ShutdownTimeout: time.Millisecond})
	defer cleanup()
	if _, ok := sink.(*newRelicSink); !ok {
		t.Fatalf("sink = %T, want *newRelicSink", sink)
	}
}

func TestEventAttributesContainOnlyAggregateClassifications(t *testing.T) {
	up := upAttributes(UpEvent{
		ComputeFamily: "balanced", ComputeSize: "medium", SelectionSource: "customized",
		ProvisioningOutcome: "succeeded", Success: true, Duration: 125 * time.Millisecond,
	})
	wantUp := map[string]interface{}{
		"compute_family": "balanced", "compute_size": "medium", "selection_source": "customized",
		"provisioning_outcome": "succeeded", "success": true, "duration_ms": int64(125),
	}
	if !reflect.DeepEqual(up, wantUp) {
		t.Fatalf("up attributes = %#v, want %#v", up, wantUp)
	}

	diagnostic := diagnosticAttributes(DiagnosticEvent{Command: "provision", Category: "ansible_failure"})
	wantDiagnostic := map[string]interface{}{"command": "provision", "category": "ansible_failure"}
	if !reflect.DeepEqual(diagnostic, wantDiagnostic) {
		t.Fatalf("diagnostic attributes = %#v, want %#v", diagnostic, wantDiagnostic)
	}
}

func TestCleanAttributeCollapsesHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "/home/private-user")
	got := cleanAttribute("app /home/private-user/config")
	if got != "app ~/config" {
		t.Fatalf("cleanAttribute = %q", got)
	}
}

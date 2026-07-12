package cmd

import (
	"context"
	"io"
	"testing"

	"github.com/mitchell-wallace/chassis/remember"
)

func TestSelectUpComputePromptsForImplicitValues(t *testing.T) {
	original := runRememberPrompt
	t.Cleanup(func() { runRememberPrompt = original })

	var labels []string
	var remembered []string
	runRememberPrompt = func(_ context.Context, _ io.Reader, _ io.Writer, config remember.Config) (string, error) {
		labels = append(labels, config.Label)
		remembered = append(remembered, *config.Remembered)
		if config.Label == "Compute family" {
			return "balanced", nil
		}
		return "medium", nil
	}

	family, size, err := selectUpCompute(context.Background(), "burstable", "xsmall", false, false, false, true)
	if err != nil {
		t.Fatalf("selectUpCompute: %v", err)
	}
	if family != "balanced" || size != "medium" {
		t.Fatalf("selection = %s/%s, want balanced/medium", family, size)
	}
	if len(labels) != 2 || labels[0] != "Compute family" || labels[1] != "Compute size" {
		t.Fatalf("prompt labels = %v", labels)
	}
	if len(remembered) != 2 || remembered[0] != "burstable" || remembered[1] != "small" {
		t.Fatalf("remembered defaults = %v, want [burstable small]", remembered)
	}
}

func TestSelectUpComputeNeverPromptsForAutomation(t *testing.T) {
	original := runRememberPrompt
	t.Cleanup(func() { runRememberPrompt = original })

	runRememberPrompt = func(context.Context, io.Reader, io.Writer, remember.Config) (string, error) {
		t.Fatal("unexpected remember prompt")
		return "", nil
	}

	tests := []struct {
		name        string
		familySet   bool
		sizeSet     bool
		assumeYes   bool
		interactive bool
	}{
		{name: "explicit family", familySet: true, interactive: true},
		{name: "explicit size", sizeSet: true, interactive: true},
		{name: "assume yes on a terminal", assumeYes: true, interactive: true},
		{name: "non-interactive stdin", assumeYes: false, interactive: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family, size, err := selectUpCompute(context.Background(), "burstable", "small", tt.familySet, tt.sizeSet, tt.assumeYes, tt.interactive)
			if err != nil {
				t.Fatalf("selectUpCompute: %v", err)
			}
			if family != "burstable" || size != "small" {
				t.Fatalf("selection = %s/%s, want unchanged burstable/small", family, size)
			}
		})
	}
}

func TestSizeRememberChoicesFollowFamilyAvailability(t *testing.T) {
	choices := sizeRememberChoices("balanced")
	if len(choices) != 3 {
		t.Fatalf("balanced choices = %d, want 3", len(choices))
	}
	for _, choice := range choices {
		if choice.Value == "xsmall" {
			t.Fatal("balanced choices unexpectedly include xsmall")
		}
	}
}

func TestUpSelectionSource(t *testing.T) {
	tests := []struct {
		name                                                 string
		beforeFamily, beforeSize, family, size               string
		familyExplicit, sizeExplicit, assumeYes, interactive bool
		want                                                 string
	}{
		{name: "remembered prompt choices", beforeFamily: "burstable", beforeSize: "small", family: "burstable", size: "small", interactive: true, want: "remembered"},
		{name: "customized prompt choice", beforeFamily: "burstable", beforeSize: "small", family: "balanced", size: "small", interactive: true, want: "customized"},
		{name: "explicit family flag", beforeFamily: "burstable", beforeSize: "small", family: "balanced", size: "small", familyExplicit: true, interactive: true, want: "explicit-flag"},
		{name: "explicit size argument", beforeFamily: "burstable", beforeSize: "small", family: "burstable", size: "large", sizeExplicit: true, want: "explicit-flag"},
		{name: "automation uses remembered config", beforeFamily: "burstable", beforeSize: "small", family: "burstable", size: "small", assumeYes: true, interactive: true, want: "remembered"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := upSelectionSource(tt.beforeFamily, tt.beforeSize, tt.family, tt.size, tt.familyExplicit, tt.sizeExplicit, tt.assumeYes, tt.interactive)
			if got != tt.want {
				t.Fatalf("upSelectionSource = %q, want %q", got, tt.want)
			}
		})
	}
}

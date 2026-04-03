package agent

import (
	"strings"
	"testing"
)

func TestContrastExamples_AlwaysIncluded(t *testing.T) {
	if !strings.Contains(contrastExamplesCore, "Over-engineering") {
		t.Fatal("missing over-engineering example")
	}
	if !strings.Contains(contrastExamplesCore, "Narrating instead") {
		t.Fatal("missing narrating example")
	}
	if !strings.Contains(contrastExamplesCore, "Claiming completion") {
		t.Fatal("missing completion example")
	}
	if !strings.Contains(contrastExamplesCore, "Defaulting to coding") {
		t.Fatal("missing coding-default example")
	}
}

func TestContrastExamples_CloudPairNotInCore(t *testing.T) {
	if strings.Contains(contrastExamplesCore, "cloud_delegate") {
		t.Fatal("cloud/local boundary example should not be in core block")
	}
}

func TestContrastExamples_CloudPairSeparate(t *testing.T) {
	if !strings.Contains(contrastExamplesCloud, "cloud_delegate") {
		t.Fatal("cloud/local boundary example missing from cloud block")
	}
}

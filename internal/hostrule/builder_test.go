package hostrule

import (
	"testing"
)

func TestBuild(t *testing.T) {
	hr := Build("api", "worker-01", "lab")
	if hr.Host != "api.worker-01.lab" {
		t.Errorf("Expected host api.worker-01.lab, got %s", hr.Host)
	}
	if hr.Rule != "Host(`api.worker-01.lab`)" {
		t.Errorf("Expected rule Host(`api.worker-01.lab`), got %s", hr.Rule)
	}
	if hr.HasCustom {
		t.Error("Expected HasCustom to be false")
	}
}

func TestBuildFromLabels_CustomHost(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.host": "custom.domain.lab",
	}
	hr := BuildFromLabels("api", "worker-01", "lab", labels)
	if hr.Host != "custom.domain.lab" {
		t.Errorf("Expected custom.domain.lab, got %s", hr.Host)
	}
	if !hr.HasCustom {
		t.Error("Expected HasCustom to be true")
	}
}

func TestBuildFromLabels_NoOverride(t *testing.T) {
	labels := map[string]string{}
	hr := BuildFromLabels("api", "worker-01", "lab", labels)
	if hr.Host != "api.worker-01.lab" {
		t.Errorf("Expected api.worker-01.lab, got %s", hr.Host)
	}
	if hr.HasCustom {
		t.Error("Expected HasCustom to be false")
	}
}

func TestBuild_CustomSuffix(t *testing.T) {
	hr := Build("web", "node-01", "example.com")
	if hr.Host != "web.node-01.example.com" {
		t.Errorf("Expected web.node-01.example.com, got %s", hr.Host)
	}
}

func TestBuild_EmptySuffixDefaultsToLab(t *testing.T) {
	hr := Build("api", "node-01", "")
	if hr.Host != "api.node-01.lab" {
		t.Errorf("Expected api.node-01.lab with default suffix, got %s", hr.Host)
	}
}

func TestBuildFromLabels_NilLabels(t *testing.T) {
	hr := BuildFromLabels("api", "worker-01", "lab", nil)
	if hr.Host != "api.worker-01.lab" {
		t.Errorf("Expected api.worker-01.lab, got %s", hr.Host)
	}
	if hr.HasCustom {
		t.Error("Expected HasCustom to be false")
	}
}

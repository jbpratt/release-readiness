package konflux

import (
	"testing"
)

func TestConvert(t *testing.T) {
	spec := SnapshotSpec{
		Application: "quay-3-17",
	}
	spec.Components = append(spec.Components, struct {
		Name           string `json:"name"`
		ContainerImage string `json:"containerImage"`
		Source         struct {
			Git struct {
				URL      string `json:"url"`
				Revision string `json:"revision"`
			} `json:"git"`
		} `json:"source"`
	}{
		Name:           "quay-server",
		ContainerImage: "quay.io/quay/quay@sha256:abc123",
	})
	spec.Components[0].Source.Git.URL = "https://github.com/quay/quay"
	spec.Components[0].Source.Git.Revision = "abc123def456"

	snap := Convert(spec, "my-snapshot-name")

	if snap.Application != "quay-3-17" {
		t.Errorf("Application = %q, want %q", snap.Application, "quay-3-17")
	}
	if snap.Snapshot != "my-snapshot-name" {
		t.Errorf("Snapshot = %q, want %q", snap.Snapshot, "my-snapshot-name")
	}
	if len(snap.Components) != 1 {
		t.Fatalf("len(Components) = %d, want 1", len(snap.Components))
	}
	c := snap.Components[0]
	if c.Name != "quay-server" {
		t.Errorf("Component.Name = %q, want %q", c.Name, "quay-server")
	}
	if c.ContainerImage != "quay.io/quay/quay@sha256:abc123" {
		t.Errorf("Component.ContainerImage = %q", c.ContainerImage)
	}
	if c.GitRevision != "abc123def456" {
		t.Errorf("Component.GitRevision = %q", c.GitRevision)
	}
	if c.GitURL != "https://github.com/quay/quay" {
		t.Errorf("Component.GitURL = %q", c.GitURL)
	}
}

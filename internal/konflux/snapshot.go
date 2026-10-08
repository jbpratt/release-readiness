package konflux

import (
	"github.com/quay/release-readiness/internal/model"
)

// SnapshotSpec is the spec section of a Konflux Snapshot CR, not the full
// Kubernetes resource.
type SnapshotSpec struct {
	Application string `json:"application"`
	Components  []struct {
		Name           string `json:"name"`
		ContainerImage string `json:"containerImage"`
		Source         struct {
			Git struct {
				URL      string `json:"url"`
				Revision string `json:"revision"`
			} `json:"git"`
		} `json:"source"`
	} `json:"components"`
}

// Convert transforms a SnapshotSpec into a model.Snapshot.
// The name parameter is the Snapshot's metadata.name, which the spec does
// not include.
func Convert(spec SnapshotSpec, name string) model.Snapshot {
	snap := model.Snapshot{
		Application: spec.Application,
		Snapshot:    name,
	}

	for _, c := range spec.Components {
		snap.Components = append(snap.Components, model.SnapshotComponent{
			Name:           c.Name,
			ContainerImage: c.ContainerImage,
			GitRevision:    c.Source.Git.Revision,
			GitURL:         c.Source.Git.URL,
		})
	}

	return snap
}

package konflux

import (
	"net/url"
	"time"

	"github.com/quay/release-readiness/internal/model"
)

// SnapshotSpec is the spec section of a Konflux Snapshot CR, as stored in S3
// or returned by the Kubernetes API.
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

// Convert transforms a SnapshotSpec into a model.Snapshot. The spec does not
// carry the snapshot name, so callers pass it: the S3 directory name or the
// CR's metadata.name.
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

// rawSnapshot is one appstudio.redhat.com/v1alpha1 Snapshot as returned by the Kubernetes API.
type rawSnapshot struct {
	Metadata struct {
		Name              string    `json:"name"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec SnapshotSpec `json:"spec"`
}

func snapshotsPath(namespace string) string {
	return "/apis/appstudio.redhat.com/v1alpha1/namespaces/" + url.PathEscape(namespace) + "/snapshots"
}

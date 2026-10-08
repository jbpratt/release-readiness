package model

// Snapshot represents the parsed state of a Konflux Snapshot.
type Snapshot struct {
	Application string              `json:"application"`
	Snapshot    string              `json:"snapshot"`
	Components  []SnapshotComponent `json:"components"`
}

// SnapshotComponent is a single component image captured in the snapshot.
type SnapshotComponent struct {
	Name           string `json:"name"`
	ContainerImage string `json:"container_image"`
	GitRevision    string `json:"git_revision"`
	GitURL         string `json:"git_url"`
}

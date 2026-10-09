package model

import "time"

type Component struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type ComponentRecord struct {
	ID         int64  `json:"id"`
	SnapshotID int64  `json:"snapshot_id"`
	Component  string `json:"component"`
	GitSHA     string `json:"git_sha"`
	ImageURL   string `json:"image_url"`
	GitURL     string `json:"git_url"`
}

type SnapshotRecord struct {
	ID          int64             `json:"id"`
	Application string            `json:"application"`
	Name        string            `json:"name"`
	CreatedAt   time.Time         `json:"created_at"`
	Components  []ComponentRecord `json:"components,omitempty"`
}

// JiraIssueRecord represents a JIRA issue cached in the database.
type JiraIssueRecord struct {
	ID         int64     `json:"id"`
	Key        string    `json:"key"`
	Summary    string    `json:"summary"`
	Status     string    `json:"status"`
	Priority   string    `json:"priority"`
	Labels     string    `json:"labels"` // comma-separated
	FixVersion string    `json:"fix_version"`
	Assignee   string    `json:"assignee"`
	IssueType  string    `json:"issue_type"`
	Resolution string    `json:"resolution"`
	Link       string    `json:"link"`
	QAContact  string    `json:"qa_contact"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IssueSummary provides aggregate counts of JIRA issues for a release.
type IssueSummary struct {
	Total    int `json:"total"`
	Verified int `json:"verified"`
	Open     int `json:"open"`
	CVEs     int `json:"cves"`
	Bugs     int `json:"bugs"`
}

// ReleaseOverview is a combined view of a release with its issue summary,
// readiness signal, and the size and age of its current component set.
type ReleaseOverview struct {
	Release        ReleaseVersion    `json:"release"`
	IssueSummary   *IssueSummary     `json:"issue_summary,omitempty"`
	Readiness      ReadinessResponse `json:"readiness"`
	ComponentCount int               `json:"component_count"`
	LatestBuild    *time.Time        `json:"latest_build,omitempty"`
	// Shipped is set when the catalog published the version's tag
	// (ShippedSource "catalog") or JIRA marks it released ("jira").
	Shipped       bool   `json:"shipped"`
	ShippedSource string `json:"shipped_source"`
	// NextInStream marks the lowest unshipped z of its product and major.minor.
	NextInStream bool `json:"next_in_stream"`
}

// ReleaseComponents is the newest image per component across a release's
// Konflux applications. The images may come from different snapshots, so
// together they are a candidate set, not one coherent build.
type ReleaseComponents struct {
	Release    string             `json:"release"`
	AsOf       *time.Time         `json:"as_of,omitempty"`
	Components []ReleaseComponent `json:"components"`
}

// ReleaseComponent is one component image with the snapshot it came from.
type ReleaseComponent struct {
	Name        string    `json:"name"`
	Image       string    `json:"image"`
	GitSHA      string    `json:"git_sha"`
	GitURL      string    `json:"git_url"`
	Application string    `json:"application"`
	Snapshot    string    `json:"snapshot"`
	CreatedAt   time.Time `json:"created_at"`
	Art         *ArtBuild `json:"art"`
}

// ReleaseSnapshotPage is one newest-first page of a release's Snapshots.
type ReleaseSnapshotPage struct {
	Snapshots []ReleaseSnapshot `json:"snapshots"`
	HasMore   bool              `json:"has_more"`
}

// ReleaseSnapshot is one Konflux Snapshot of a release's applications with the
// Konflux Releases that name it. Missing marks a Snapshot a Release names that
// is no longer stored; its CreatedAt is then the oldest Release's.
type ReleaseSnapshot struct {
	Application    string           `json:"application"`
	Name           string           `json:"name"`
	CreatedAt      time.Time        `json:"created_at"`
	ComponentCount int              `json:"component_count"`
	Missing        bool             `json:"missing,omitempty"`
	Releases       []KonfluxRelease `json:"releases,omitempty"`
	Components     []SnapshotImage  `json:"components,omitempty"`
	FBCCatalog     *FBCCatalog      `json:"fbc_catalog,omitempty"`
}

// FBCCatalog compares a Quay Snapshot's quay-operator bundle with the
// stable-X.Y channel of the newest quay-operator FBC catalog of its release.
// Status is "current" when the channel references the bundle's digest,
// "behind" when a fully read channel of digest refs does not, else "unknown".
type FBCCatalog struct {
	Status              string `json:"status"`
	CatalogSnapshot     string `json:"catalog_snapshot"`
	CatalogImage        string `json:"catalog_image"`
	CatalogBundleImage  string `json:"catalog_bundle_image"`
	SnapshotBundleImage string `json:"snapshot_bundle_image"`
}

// SnapshotImage is one component image exactly as a Snapshot records it.
type SnapshotImage struct {
	Name            string           `json:"name"`
	Image           string           `json:"image"`
	GitSHA          string           `json:"git_sha"`
	GitURL          string           `json:"git_url"`
	Art             *ArtBuild        `json:"art"`
	PendingArtBuild *PendingArtBuild `json:"pending_art_build"`
}

// ArtBuild links a component image to its ART build history record and the
// upstream commit it was built from.
type ArtBuild struct {
	NVR          string `json:"nvr"`
	BuildURL     string `json:"build_url"`
	LogsURL      string `json:"logs_url"`
	PipelineURL  string `json:"pipeline_url"`
	UpstreamRepo string `json:"upstream_repo"`
	UpstreamSHA  string `json:"upstream_sha"`
}

// PendingArtBuild is a newer ART build of a component, still running, from a
// different upstream commit than the Snapshot's image.
type PendingArtBuild struct {
	BuildURL    string    `json:"build_url"`
	UpstreamSHA string    `json:"upstream_sha"`
	StartedAt   time.Time `json:"started_at"`
}

// ReadinessResponse represents the computed readiness signal for a release.
type ReadinessResponse struct {
	Signal  string `json:"signal"`  // "green", "yellow", "red"
	Message string `json:"message"` // human-readable reason
}

// ReleaseVersion represents a JIRA fixVersion with release metadata.
type ReleaseVersion struct {
	Name                  string     `json:"name"`
	Description           string     `json:"description"`
	ReleaseDate           *time.Time `json:"release_date,omitempty"`
	Released              bool       `json:"released"`
	Archived              bool       `json:"archived"`
	ReleaseTicketKey      string     `json:"release_ticket_key,omitempty"`
	ReleaseTicketAssignee string     `json:"release_ticket_assignee,omitempty"`
	KonfluxApplication    string     `json:"konflux_application,omitempty"`
	DueDate               *time.Time `json:"due_date,omitempty"`
}

// KonfluxRelease is a Konflux Release custom resource, distinct from the JIRA
// ReleaseVersion.
type KonfluxRelease struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Application    string     `json:"application"`
	Snapshot       string     `json:"snapshot"`
	ReleasePlan    string     `json:"release_plan"`
	Target         string     `json:"target"`
	ReleasedStatus string     `json:"released_status"`
	ReleasedReason string     `json:"released_reason"`
	FailedTask     string     `json:"failed_task,omitempty"`
	FailedStep     string     `json:"failed_step,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartTime      *time.Time `json:"start_time,omitempty"`
	CompletionTime *time.Time `json:"completion_time,omitempty"`
}

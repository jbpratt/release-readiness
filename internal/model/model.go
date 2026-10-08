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

type ApplicationSummary struct {
	Application    string          `json:"application"`
	LatestSnapshot *SnapshotRecord `json:"latest_snapshot,omitempty"`
	SnapshotCount  int             `json:"snapshot_count"`
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
// readiness signal, and latest snapshot metadata.
type ReleaseOverview struct {
	Release      ReleaseVersion    `json:"release"`
	IssueSummary *IssueSummary     `json:"issue_summary,omitempty"`
	Readiness    ReadinessResponse `json:"readiness"`
	Snapshot     *SnapshotRecord   `json:"snapshot,omitempty"`
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
	S3Application         string     `json:"s3_application,omitempty"`
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
	CreatedAt      time.Time  `json:"created_at"`
	StartTime      *time.Time `json:"start_time,omitempty"`
	CompletionTime *time.Time `json:"completion_time,omitempty"`
}

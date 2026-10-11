package model

import "time"

// JiraIssueRecord represents a JIRA issue cached in the database.
type JiraIssueRecord struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	Labels     string `json:"-"` // comma-separated
	FixVersion string `json:"fix_version"`
	Assignee   string `json:"assignee"`
	IssueType  string `json:"issue_type"`
	Link       string `json:"link"`
	QAContact  string `json:"qa_contact"`
}

// IssueSummary provides aggregate counts of JIRA issues for a release.
type IssueSummary struct {
	Total    int `json:"total"`
	Verified int `json:"verified"`
	Open     int `json:"open"`
	CVEs     int `json:"cves"`
}

// ReleaseOverview is a combined view of a release with its issue summary,
// readiness signal, and when its newest build, stream or STAGE, was created.
type ReleaseOverview struct {
	Release      ReleaseVersion    `json:"release"`
	IssueSummary *IssueSummary     `json:"issue_summary,omitempty"`
	Readiness    ReadinessResponse `json:"readiness"`
	LatestBuild  *time.Time        `json:"latest_build,omitempty"`
	// Shipped is set when the catalog published the version's tag or JIRA
	// marks it released.
	Shipped bool `json:"shipped"`
	// NextInStream marks the lowest unshipped z of its product and major.minor.
	NextInStream bool `json:"next_in_stream"`
}

// ReleaseSnapshot is one Konflux Snapshot of a release's applications.
type ReleaseSnapshot struct {
	Name       string
	CreatedAt  time.Time
	Components []SnapshotImage
}

// SnapshotImage is one component image exactly as a Snapshot records it.
type SnapshotImage struct {
	Name, Image string
}

// ReadinessResponse represents the computed readiness signal for a release.
type ReadinessResponse struct {
	Signal  string `json:"signal"`  // "green", "yellow", "red"
	Message string `json:"message"` // human-readable reason
	// Shipped is set when JIRA marks it released or the catalog published it.
	Shipped bool `json:"shipped"`
}

// ReleaseVersion represents a JIRA fixVersion with release metadata.
type ReleaseVersion struct {
	Name                  string     `json:"name"`
	ReleaseDate           *time.Time `json:"release_date,omitempty"`
	Released              bool       `json:"released"`
	Archived              bool       `json:"-"`
	ReleaseTicketKey      string     `json:"release_ticket_key,omitempty"`
	ReleaseTicketAssignee string     `json:"release_ticket_assignee,omitempty"`
	KonfluxApplication    string     `json:"konflux_application,omitempty"`
	DueDate               *time.Time `json:"due_date,omitempty"`
}

// KonfluxRelease is a Konflux Release custom resource, distinct from the JIRA
// ReleaseVersion.
type KonfluxRelease struct {
	Name           string     `json:"name"`
	Application    string     `json:"-"`
	Snapshot       string     `json:"-"`
	ReleasePlan    string     `json:"release_plan"`
	ReleasedStatus string     `json:"released_status"`
	ReleasedReason string     `json:"released_reason"`
	FailedTask     string     `json:"failed_task,omitempty"`
	FailedStep     string     `json:"failed_step,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartTime      *time.Time `json:"start_time,omitempty"`
	CompletionTime *time.Time `json:"completion_time,omitempty"`
}

// SelectedBuild is a staged image Snapshot of a concrete version whose
// Konflux Release through a STAGE ReleasePlan succeeded.
// CompletedAt is that Release's completion time.
type SelectedBuild struct {
	SnapshotName string
	CompletedAt  time.Time
}

// TicketBuild is the build a version's tickets are checked against, its
// candidate: Source "staged" is its selected STAGE build, "newest" its newest
// build. Since is the previous version of its X.Y when that one is unshipped
// and has a candidate, whose commits are the bases the .z tickets are found
// from, else "". Targets is set when the version has Target Version tickets.
type TicketBuild struct {
	Snapshot   string            `json:"snapshot"`
	Source     string            `json:"source"`
	Since      string            `json:"-"`
	Targets    bool              `json:"-"`
	Components []TicketComponent `json:"-"`
}

// TicketComponent is one image of a ticket build with the upstream commit ART
// built it from, both empty until ART resolves it, and BaseSHA, the upstream
// commit of the component of the same name in Since's candidate.
type TicketComponent struct {
	Name, UpstreamRepo, UpstreamSHA, BaseSHA string
}

// BuildTickets is a release's tickets with the commits of its candidate build
// that name each. Build is nil with Reason set when there is none. ZSince is
// the build's Since when its commits were the base of at least one component.
type BuildTickets struct {
	Build       *TicketBuild  `json:"build"`
	Reason      string        `json:"reason,omitempty"`
	ZSince      string        `json:"z_since,omitempty"`
	NotCompared []NotCompared `json:"not_compared"`
	Tickets     []BuildTicket `json:"tickets"`
}

// NotCompared is a build component whose commits were not read.
type NotCompared struct {
	Component string `json:"component"`
	Reason    string `json:"reason"`
}

// BuildTicket is a JIRA issue with the build commits whose message names it
// and, for a Target Version ticket none names, the default-branch commits the
// build lacks that name it.
type BuildTicket struct {
	JiraIssueRecord
	InBuild    []BuildCommit `json:"in_build"`
	NotInBuild []BuildCommit `json:"not_in_build"`
}

// BuildCommit is an upstream commit in, or missing from, one component of a
// build.
type BuildCommit struct {
	Component string `json:"component"`
	CommitSHA string `json:"commit_sha"`
	CommitURL string `json:"commit_url"`
}

// StageRelease is a Konflux Release through a STAGE ReleasePlan with the
// component images of the Snapshot it names.
type StageRelease struct {
	KonfluxRelease
	Images []string
}

// ReleaseCandidate is what decides whether a version can ship: its release
// candidate, the selected STAGE build, or null with Reason set when there is
// none, and its newest build when that holds, for a component the candidate
// holds, another image than the candidate does, or when there is no
// candidate. A shipped version sets nothing else.
type ReleaseCandidate struct {
	Shipped   bool            `json:"shipped"`
	Candidate *CandidateBuild `json:"candidate"`
	Reason    string          `json:"reason"`
	Newest    *NewestBuild    `json:"newest"`
}

// CandidateBuild is the Snapshot up for release: Source "staged" is the
// version's selected STAGE build, "newest" its newest build, the newest stream
// build whose images' NVRs carry the version. A STAGE build, the release
// candidate, has whether it reached stage, the version's prod Release and, per
// job, the periodic CI runs that installed its operator bundle.
type CandidateBuild struct {
	Snapshot   string               `json:"snapshot"`
	Source     string               `json:"-"`
	CreatedAt  time.Time            `json:"created_at"`
	Components []CandidateComponent `json:"components"`
	Stage      StageFlag            `json:"stage"`
	Prod       *KonfluxRelease      `json:"prod"`
	CI         []CIJob              `json:"ci"`
}

// NewestBuild is a version's newest build. Stage is the State of its stage
// flag over its images of the components the candidate holds, or over all of
// them when there is no candidate.
type NewestBuild struct {
	Snapshot  string    `json:"snapshot"`
	CreatedAt time.Time `json:"created_at"`
	Stage     string    `json:"stage"`
}

// CandidateComponent is a build image with its ART build and the upstream
// commit ART built it from, all empty until ART resolves it, and its CVE scan,
// null until the scan sync reads it.
type CandidateComponent struct {
	Name                      string     `json:"name"`
	Image                     string     `json:"image"`
	NVR                       string     `json:"nvr"`
	BuildURL                  string     `json:"build_url"`
	Scan                      *ImageScan `json:"scan"`
	UpstreamRepo, UpstreamSHA string     `json:"-"`
}

// ImageScan is the CVE scan of an image by its build PipelineRun: State is
// "pending", "scanned", "scan_failed" or "not_scanned", and Counts is null
// unless scanned. URL is the PipelineRun's Konflux page.
type ImageScan struct {
	State  string      `json:"state"`
	Counts *ScanCounts `json:"counts"`
	URL    string      `json:"url"`
}

// ScanCounts is a scan's CVEs by severity, split by whether a fix exists.
// Basis "scanner_arch_findings" counts a CVE once per architecture it is
// found in; "unique_cves" counts it once.
type ScanCounts struct {
	Basis   string         `json:"basis"`
	Fixable SeverityCounts `json:"fixable"`
	NoFix   SeverityCounts `json:"no_fix"`
}

type SeverityCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// StageFlag is the release candidate's stage: State "staged", StagedAt when
// its stage Release completed, and Total its images. Its stage Release
// succeeded with every image of it, so the lists are empty and Release null.
type StageFlag struct {
	State     string          `json:"state"`
	StagedAt  *time.Time      `json:"staged_at"`
	Total     int             `json:"total"`
	NotStaged []string        `json:"not_staged"`
	Release   *KonfluxRelease `json:"release"`
	NoBundle  []string        `json:"no_bundle"`
	NoRelease []string        `json:"no_release"`
}

// CIJob is a job's newest run of a build and how many of its runs tested it.
type CIJob struct {
	JobName   string     `json:"job_name"`
	State     string     `json:"state"`
	ProwURL   string     `json:"prow_url"`
	StartedAt *time.Time `json:"started_at"`
	Runs      int        `json:"runs"`
}

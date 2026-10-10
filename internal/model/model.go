package model

import "time"

type ComponentRecord struct {
	Component, ImageURL string
}

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
// readiness signal, and the age of its current component set.
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

// ReleaseSnapshotPage is one newest-first page of a release's Snapshots.
type ReleaseSnapshotPage struct {
	Snapshots []ReleaseSnapshot `json:"snapshots"`
	HasMore   bool              `json:"has_more"`
}

// ReleaseSnapshot is one Konflux Snapshot of a release's applications with the
// Konflux Releases that name it. Missing marks a Snapshot a Release names that
// is no longer stored; its CreatedAt is then the oldest Release's. ArtKind is
// the art.redhat.com/kind (image or fbc) of a Snapshot ART built for an
// assembly; only lists set it.
type ReleaseSnapshot struct {
	Application    string           `json:"-"`
	Name           string           `json:"name"`
	CreatedAt      time.Time        `json:"created_at"`
	ComponentCount int              `json:"component_count"`
	Missing        bool             `json:"missing,omitempty"`
	ArtKind        string           `json:"art_kind,omitempty"`
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
	CatalogBundleImage  string `json:"catalog_bundle_image"`
	SnapshotBundleImage string `json:"snapshot_bundle_image"`
}

// SnapshotImage is one component image exactly as a Snapshot records it.
type SnapshotImage struct {
	Name            string           `json:"name"`
	Image           string           `json:"image"`
	Art             *ArtBuild        `json:"art"`
	PendingArtBuild *PendingArtBuild `json:"pending_art_build"`
}

// ArtBuild links a component image to its ART build history record and the
// upstream commit it was built from.
type ArtBuild struct {
	NVR          string `json:"nvr"`
	BuildURL     string `json:"build_url"`
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

// BuildAttempts is a release's ART image-build attempts whose start time
// falls in the span ART history was read without a gap, newest first.
// CoveredFrom is null when it was never read.
type BuildAttempts struct {
	CoveredFrom *time.Time     `json:"covered_from"`
	Attempts    []BuildAttempt `json:"attempts"`
}

// BuildAttempt is one ART image build. Component is its NVR name.
type BuildAttempt struct {
	Component string    `json:"component"`
	Outcome   string    `json:"outcome"`
	StartedAt time.Time `json:"started_at"`
	BuildURL  string    `json:"build_url"`
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

// StagedSnapshots is the newest image Konflux Snapshot and each operator's
// newest FBC Snapshot that ART staged for a release's assembly; each is null
// when ART staged none. StreamStaged is whether ART staged any version of the
// release's X.Y stream.
type StagedSnapshots struct {
	StreamStaged bool            `json:"stream_staged"`
	Image        *StagedSnapshot `json:"staged_image"`
	Catalogs     []StagedCatalog `json:"catalogs"`
}

// StagedCatalog is an operator's newest staged FBC Snapshot.
type StagedCatalog struct {
	Operator string          `json:"operator"`
	Staged   *StagedSnapshot `json:"staged"`
}

// StagedSnapshot is a Snapshot annotated art.redhat.com/env=stage with the
// newest Konflux Release that names it, or null when none does.
type StagedSnapshot struct {
	Name      string          `json:"name"`
	CreatedAt time.Time       `json:"created_at"`
	Release   *KonfluxRelease `json:"release"`
}

// SelectedBuild is a staged image Snapshot of a concrete version whose
// Konflux Release through a STAGE ReleasePlan succeeded.
// CompletedAt is that Release's completion time.
type SelectedBuild struct {
	SnapshotName string                   `json:"snapshot"`
	CompletedAt  time.Time                `json:"completed_at"`
	Components   []SelectedBuildComponent `json:"-"`
}

// SelectedBuildComponent is one image of a selected build with the upstream
// commit ART built it from; both are empty when ART has not resolved it.
type SelectedBuildComponent struct {
	Name, UpstreamRepo, UpstreamSHA string
}

// TicketBuild is the build a version's tickets are checked against, its
// candidate: Source "staged" is its selected STAGE build, "newest" the
// stream's newest build. Since is the previous version of its X.Y when that
// one is unshipped and has a candidate, whose commits are the bases the .z
// tickets are found from, else "". Targets is set when the version has
// Target Version tickets.
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

// ReleaseCandidate is what decides whether a version can ship: the build up
// for release and, per build that matters, whether it reached stage, the
// version's prod Release and the periodic CI runs that tested it. Candidate
// is null with Reason set, and Builds empty, when there is none. A shipped
// version sets nothing else.
type ReleaseCandidate struct {
	Shipped   bool            `json:"shipped"`
	Candidate *CandidateBuild `json:"candidate"`
	Reason    string          `json:"reason"`
	Builds    []BuildRow      `json:"builds"`
}

// CandidateBuild is the Snapshot up for release: Source "staged" is the
// version's selected STAGE build, "newest" the stream's newest build.
type CandidateBuild struct {
	Snapshot   string               `json:"snapshot"`
	Source     string               `json:"source"`
	CreatedAt  time.Time            `json:"created_at"`
	Components []CandidateComponent `json:"components"`
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

// BuildRow is a build that matters to a version. Roles are "candidate", the
// build up for release, "newest", the stream's newest build, and
// "last_tested", the newest build a periodic run tested, listed only when
// neither of the others was. Prod, the version's prod Release, is set on the
// candidate only. CI is, per job, the periodic runs that tested the build.
type BuildRow struct {
	Snapshot  string          `json:"snapshot"`
	CreatedAt time.Time       `json:"created_at"`
	Roles     []string        `json:"roles"`
	Stage     StageFlag       `json:"stage"`
	Prod      *KonfluxRelease `json:"prod"`
	CI        []CIJob         `json:"ci"`
}

// StageFlag tells whether every image of a build is in a Snapshot a STAGE
// Release of the stream succeeded with: State is "staged", with StagedAt when
// the build reached stage, "not_staged" with the components that are not, or
// "unknown" when the stream has no STAGE Release. Release is the newest STAGE
// Release that did not succeed whose Snapshot holds an image that is not staged.
type StageFlag struct {
	State     string          `json:"state"`
	StagedAt  *time.Time      `json:"staged_at"`
	Total     int             `json:"total"`
	NotStaged []string        `json:"not_staged"`
	Release   *KonfluxRelease `json:"release"`
}

// CIJob is a job's newest run of a build and how many of its runs tested it.
type CIJob struct {
	JobName   string     `json:"job_name"`
	State     string     `json:"state"`
	ProwURL   string     `json:"prow_url"`
	StartedAt *time.Time `json:"started_at"`
	Runs      int        `json:"runs"`
}

package model

import "time"

// JiraIssueRecord represents a JIRA issue cached in the database.
type JiraIssueRecord struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	FixVersion string `json:"-"`
	IssueType  string `json:"issue_type"`
	Link       string `json:"link"`
}

// ReleaseOverview is a combined view of a release with its readiness signal
// and when its newest build, stream or STAGE, was created.
type ReleaseOverview struct {
	Release     ReleaseVersion    `json:"release"`
	Readiness   ReadinessResponse `json:"readiness"`
	LatestBuild *time.Time        `json:"latest_build,omitempty"`
	// Shipped is set when the catalog published the version's tag or JIRA
	// marks it released.
	Shipped bool `json:"-"`
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
	Signal  string `json:"signal"`  // "green", "yellow", "red", or "" if nothing to flag
	Message string `json:"message"` // human-readable reason
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
	ReleasePlan    string     `json:"-"`
	ReleasedStatus string     `json:"released_status"`
	ReleasedReason string     `json:"released_reason"`
	FailedTask     string     `json:"failed_task,omitempty"`
	FailedStep     string     `json:"failed_step,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletionTime *time.Time `json:"completion_time,omitempty"`
}

// SelectedBuild is a staged image Snapshot of a concrete version whose
// Konflux Release through a STAGE ReleasePlan succeeded.
// CompletedAt is that Release's completion time.
type SelectedBuild struct {
	SnapshotName string
	CompletedAt  time.Time
}

// StageRelease is a Konflux Release through a STAGE ReleasePlan with the
// component images of the Snapshot it names.
type StageRelease struct {
	KonfluxRelease
	Images []string
}

// ReleaseCandidate is what decides whether a version can ship: its release
// candidate, the selected STAGE build, or null with Reason set when there is
// none, with the periodic CI runs since its operator bundle was built, or
// without a candidate since its builds' were, when it has builds, and its
// newest build when that holds, for a component the candidate holds, another
// image than the candidate does, or when there is no candidate, and is not a
// build CI names to pick, re-run with or, beside the candidate, in CI. A
// shipped version keeps its candidate and CI, with no Reason, newest build, or
// build in CI to pick, re-run with or wait on.
type ReleaseCandidate struct {
	Shipped   bool            `json:"shipped"`
	Candidate *CandidateBuild `json:"candidate"`
	Reason    string          `json:"reason"`
	Newest    *NewestBuild    `json:"newest"`
	CI        *CandidateCI    `json:"ci"`
}

// CandidateBuild is the release candidate, the version's selected STAGE
// build, with whether it reached stage and the version's prod Release.
type CandidateBuild struct {
	Snapshot   string               `json:"snapshot"`
	CreatedAt  time.Time            `json:"created_at"`
	Components []CandidateComponent `json:"components"`
	Stage      StageFlag            `json:"stage"`
	Prod       *KonfluxRelease      `json:"prod"`
}

// NewestBuild is a version's newest build and whether its operator bundle
// reached the catalog CI installs from.
type NewestBuild struct {
	Snapshot  string    `json:"snapshot"`
	CreatedAt time.Time `json:"created_at"`
	CI        CIState   `json:"ci"`
}

// CIState tells whether an operator bundle reached the catalog CI installs
// from: State "in" with the per-build stage Release that succeeded with it,
// "failed" with the one that failed, "none", or "unknown" for a build without
// a bundle or a stream without such Releases.
type CIState struct {
	State   string          `json:"state"`
	Release *KonfluxRelease `json:"release"`
}

// CandidateCI is the application's periodic runs since the candidate's
// operator bundle was first built, or without a candidate since the first of
// the version's builds' bundles was, by the build each installed; the newest
// bundle of the version's builds in the CI catalog, when that is not the
// candidate's; Passed, the newest of them, newer than the candidate, that a run
// passed with, and FBC, its FBCs, null until ART's record of each of the
// version's FBC images is read; and Refresh, what to do: with a candidate
// "rerun" release-from-fbc with Passed, "waiting" while a newer build in CI
// has not passed, or "current"; without one "pick" Passed, or "none".
type CandidateCI struct {
	Since      time.Time   `json:"-"`
	Builds     []CIBuild   `json:"builds"`
	LatestInCI *BuildLabel `json:"latest_in_ci"`
	Passed     *BuildLabel `json:"passed"`
	FBC        *BuildFBC   `json:"fbc"`
	Refresh    string      `json:"refresh"`
}

// BuildFBC is a build's FBCs for release-from-fbc: of the version's FBC images
// of each operator and OCP version, by pullspec, the one ART built last that
// holds a bundle of the build, and in Missing each pair without one, both by
// operator, then OCP version.
type BuildFBC struct {
	Pullspecs []string     `json:"pullspecs"`
	Missing   []MissingFBC `json:"missing"`
}

// MissingFBC is an operator and OCP version a build has no FBC of, with URL,
// ART build history's search for the operator's FBCs, "" when ART links are
// off.
type MissingFBC struct {
	Operator string `json:"operator"`
	OCP      string `json:"ocp"`
	URL      string `json:"url"`
}

// BuildLabel names an operator bundle by the oldest stream build holding it.
type BuildLabel struct {
	Snapshot  string    `json:"snapshot"`
	CreatedAt time.Time `json:"created_at"`
}

// CIBuild is the runs, by job, that installed one build, or failed to install
// it: Kind "candidate", "build" with its label and CIState, or "unknown".
type CIBuild struct {
	Kind      string     `json:"kind"`
	Snapshot  string     `json:"snapshot"`
	CreatedAt *time.Time `json:"created_at"`
	CI        *CIState   `json:"ci"`
	Jobs      []CIJob    `json:"jobs"`
}

// CIJob is a job's runs of a build, newest first.
type CIJob struct {
	JobName string  `json:"job_name"`
	Runs    []CIRun `json:"runs"`
}

type CIRun struct {
	State         string    `json:"state"`
	StartedAt     time.Time `json:"started_at"`
	ProwURL       string    `json:"prow_url"`
	InstallFailed bool      `json:"install_failed,omitempty"`
}

// CandidateComponent is a build image with its ART build, empty until ART
// resolves it.
type CandidateComponent struct {
	Name     string `json:"name"`
	Image    string `json:"-"`
	NVR      string `json:"nvr"`
	BuildURL string `json:"build_url"`
}

// StageFlag is the release candidate's stage: StagedAt when its stage Release
// completed.
type StageFlag struct {
	StagedAt *time.Time `json:"staged_at"`
}

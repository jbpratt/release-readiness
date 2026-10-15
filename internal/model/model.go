package model

import "time"

// JiraIssueRecord represents a JIRA issue cached in the database.
type JiraIssueRecord struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	FixVersion string `json:"-"`
	IssueType  string `json:"issue_type"`
	Status     string `json:"status"`
	Link       string `json:"link"`
}

// IssueSummary provides aggregate counts of JIRA issues for a release.
type IssueSummary struct {
	CVEs int `json:"cves"`
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
	Shipped bool `json:"-"`
	// NextInStream marks the lowest unshipped z of its product and major.minor.
	NextInStream bool `json:"next_in_stream"`
	// ShipmentMR is nil until the version's shipment MR is found.
	ShipmentMR *ShipmentMR `json:"shipment_mr"`
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
	// JobURL is the ART job that made the Release, Advisory its errata URL.
	JobURL   string `json:"job_url,omitempty"`
	Advisory string `json:"advisory,omitempty"`
}

// ShipmentMR is the ART shipment merge request of a Quay version in GitLab.
// Stage, PipelineFailed and NeedsApproval derive from the other fields.
// Source is "pointer" when ocp-build-data names the MR, "search" when it was
// found by branch.
type ShipmentMR struct {
	Version             string     `json:"-"`
	Assembly            string     `json:"-"`
	IID                 int        `json:"iid"`
	WebURL              string     `json:"web_url"`
	State               string     `json:"state"`
	Draft               bool       `json:"draft"`
	Labels              []string   `json:"-"`
	Stage               string     `json:"stage"`
	PipelineFailed      bool       `json:"pipeline_failed"`
	NeedsApproval       bool       `json:"needs_approval"`
	HeadPipelineStatus  string     `json:"head_pipeline_status"`
	DetailedMergeStatus string     `json:"-"`
	MergedAt            *time.Time `json:"merged_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Source              string     `json:"source"`
	CheckedAt           time.Time  `json:"-"`
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

// ReleaseCandidate is what decides whether a version can ship: its operator
// FBCs up for release, and its STAGE build, the image Snapshot ART selected,
// or null with Reason set when there is none. A shipped version has no
// Reason.
type ReleaseCandidate struct {
	Shipped   bool            `json:"shipped"`
	Candidate *CandidateBuild `json:"candidate"`
	Reason    string          `json:"reason"`
	// Catalogs is the version's FBC catalogs, staged ones first.
	Catalogs []CandidateCatalog `json:"catalogs"`
	// UnshippedBuilds is, with a STAGE build, the newest image of the
	// version's builds of each component it leaves out.
	UnshippedBuilds []UnshippedBuild `json:"unshipped_builds"`
	// FBCCandidate is the version's operator FBCs up for release: as ART
	// staged them for stage, else as its stream built them; FBCStream is
	// always as its stream built them. Both are nil while the version has no
	// FBC built or staged.
	FBCCandidate *FBCCandidate `json:"fbc_candidate"`
	FBCStream    *FBCCandidate `json:"fbc_stream"`
	// CIHistory is, by operator and OCP version, the CI of the FBC candidate's
	// FBC of each slot. It is advisory: the verdict never reads it.
	CIHistory []CISlot     `json:"ci_history"`
	Verdict   Verdict      `json:"verdict"`
	Reading   ReadProgress `json:"reading"`
}

// Verdict tells whether a version can ship, by its FBC candidate: State
// "shipped", "not_ready" with a blocker, "checking" while the candidate's FBC
// reads or the ART history backfill are pending, else "ready". Items are its
// blockers, then warnings, then info.
type Verdict struct {
	State string        `json:"state"`
	Items []VerdictItem `json:"items"`
}

// VerdictItem is a blocker, warning or info on Operator, "" for the whole
// release, for Owner, "ART", "ART/Konflux", "Quay (release-from-fbc)",
// "Quay/ART" or "", to act on.
type VerdictItem struct {
	Level    string `json:"level"`
	Operator string `json:"operator"`
	Owner    string `json:"owner"`
	Code     string `json:"code"`
	Text     string `json:"text"`
}

// ReadProgress counts the FBC images the render sync reads by the state of
// their read, and HistoryDays the days of ART build history of the version's
// stream its backfill has yet to read.
type ReadProgress struct {
	Read        int `json:"read"`
	Failed      int `json:"failed"`
	Pending     int `json:"pending"`
	HistoryDays int `json:"history_days"`
}

// FBCCandidate is a version's operator FBCs, one per operator and OCP version
// slot: Source "stage_snapshots" takes each slot's FBC from the newest FBC
// Snapshot ART staged for stage, "stream" its newest successful ART build.
// Operators always holds quay-operator, container-security-operator and
// quay-bridge-operator; one without an FBC is in Errors. State is
// "inconsistent" when an operator's slots head different bundles,
// "incomplete" for an operator or slot without an FBC, "unknown" while a
// head bundle or a staged catalog's identity is unresolved, else "complete".
// Unknown holds, once per digest, the staged catalogs no NVR names, which
// fill no slot. Pullspecs, release-from-fbc's fbc-pullspecs by operator, then
// OCP version, is empty unless State is "complete".
type FBCCandidate struct {
	Source    string        `json:"source"`
	State     string        `json:"state"`
	Errors    []string      `json:"errors"`
	Operators []OperatorFBC `json:"operators"`
	Unknown   []FBCMember   `json:"unknown"`
	Pullspecs []string      `json:"pullspecs"`
}

// OperatorFBC is one operator's FBCs, keyed by HeadBundleNVR, the NVR of the
// bundle heading the release channel of its newest FBC that has one joined,
// "" for none, with that FBC's channel and head CSV. Members are its slots'
// FBCs on that bundle and Conflicts the others, both by OCP version;
// MissingOCP the OCP versions it was ever built or staged for without an FBC
// here, and Coverage how many of those slots Members fill. FBCRIStage tells
// whether its head bundle reached stage through a stage Release, "unknown"
// without a head bundle; ImageCheck compares that bundle with the image
// Snapshot's. Images is the head bundle's relatedImages, by name, as its
// newest-OCP member FBC with a render lists them; Compare compares them with
// that FBC's previous entry's and, for a candidate, with the stream's when
// the stream heads another bundle. Stage counts the Members whose newest stage
// Release succeeded.
type OperatorFBC struct {
	Operator      string          `json:"operator"`
	HeadBundleNVR string          `json:"head_bundle_nvr"`
	Channel       string          `json:"channel"`
	HeadCSV       string          `json:"head_csv"`
	Coverage      FBCCoverage     `json:"coverage"`
	Members       []FBCMember     `json:"members"`
	MissingOCP    []string        `json:"missing_ocp"`
	Conflicts     []FBCMember     `json:"conflicts"`
	FBCRIStage    CIState         `json:"fbc_ri_stage"`
	ImageCheck    ImageCheck      `json:"image_check"`
	Images        []OperatorImage `json:"images"`
	Compare       []ImageCompare  `json:"compare"`
	Stage         StageSummary    `json:"stage"`
}

type StageSummary struct {
	Staged int `json:"staged"`
	Total  int `json:"total"`
}

// OperatorImage is a bundle's relatedImage with its ART build: NVR, Commit
// and BuildURL (its ART build page) are "" for an image ART did not build.
// Stage is set when a succeeded stage Release holds its digest, Prod when a
// succeeded prod Release does. Scan is its CVE scan, null until the scan sync
// reads it.
type OperatorImage struct {
	Name     string     `json:"name"`
	Image    string     `json:"image"`
	Digest   string     `json:"digest"`
	NVR      string     `json:"nvr"`
	Commit   string     `json:"commit"`
	BuildURL string     `json:"build_url"`
	Stage    bool       `json:"stage"`
	Prod     bool       `json:"prod"`
	Scan     *ImageScan `json:"scan"`
}

// ImageCompare compares two bundles' relatedImages: Kind "previous", from the
// previous entry of Label's version, or "newer", to the stream's head bundle
// of NVR Label.
type ImageCompare struct {
	Kind  string            `json:"kind"`
	Label string            `json:"label"`
	Rows  []ImageCompareRow `json:"rows"`
}

// ImageCompareRow is a relatedImage's From and To, nil where absent, named as
// To names it. Change is "renamed" for another repository, "same" for one
// digest, "rebuild" for another from the same commit, "new source" from
// another commit, "new build" when either commit is unknown, or "added" or
// "removed".
type ImageCompareRow struct {
	Name   string    `json:"name"`
	From   *ImageRef `json:"from"`
	To     *ImageRef `json:"to"`
	Change string    `json:"change"`
}

type ImageRef struct {
	Repository string `json:"repository"`
	NVR        string `json:"nvr"`
	Commit     string `json:"commit"`
	Digest     string `json:"digest"`
	BuildURL   string `json:"build_url"`
}

// ImageCheck compares an operator's head bundle with the bundle of it in the
// version's image Snapshot, its STAGE build, else its newest build, found by
// NVR in any component, else as component <app>-<operator>-bundle: State
// "matches" or "differs" by NVR, or "absent" when either NVR is unknown or
// there is no image Snapshot.
type ImageCheck struct {
	State     string `json:"state"`
	Snapshot  string `json:"snapshot"`
	BundleNVR string `json:"bundle_nvr"`
}

// FBCCI is the periodic runs that installed from an FBC, by job: Exact from
// its digest, Equivalent from another FBC of its operator and OCP version
// headed by the same bundle, whose catalog can still differ. Equivalent sees
// only FBCs rendered and joined to a head bundle, and the render sync renders
// staged catalogs and each slot's newest ART build, so a run on an FBC
// superseded before a render pass never counts.
type FBCCI struct {
	Exact      []CIJob `json:"exact"`
	Equivalent []CIJob `json:"equivalent"`
}

// CISlot is an operator's OCP slot with the CI of the FBC candidate's FBC of
// it, nil without one.
type CISlot struct {
	Operator string `json:"operator"`
	OCP      string `json:"ocp"`
	CI       *FBCCI `json:"ci"`
}

type FBCCoverage struct {
	Members int `json:"members"`
	Slots   int `json:"slots"`
}

// FBCMember is an FBC catalog image: its NVR, from ART or its label, "" when
// neither names it, with its ART build page; the NVR of the bundle heading its
// release channel, "" until joined to an ART bundle build; and, when staged,
// the FBC Snapshot holding it and that Snapshot's newest Release, whatever its
// status, nil before one, in a candidate's slot the newest Snapshot whose
// Release succeeded, else failed; and, in a slot, its CI. Staged counts, in a
// candidate's slot, the staged FBC Snapshots holding a catalog of the slot
// whose newest Release succeeded; StageFailed, when no Snapshot holding its
// digest has, the newest failed one; ReadError is why reading it failed.
type FBCMember struct {
	OCP           string          `json:"ocp"`
	Digest        string          `json:"digest"`
	Pullspec      string          `json:"pullspec"`
	NVR           string          `json:"nvr"`
	BuildURL      string          `json:"build_url"`
	HeadBundleNVR string          `json:"head_bundle_nvr"`
	Snapshot      string          `json:"snapshot"`
	Release       *KonfluxRelease `json:"release"`
	CI            *FBCCI          `json:"ci"`
	Staged        int             `json:"staged"`
	StageFailed   *KonfluxRelease `json:"-"`
	RecordID      string          `json:"-"`
	ReadError     string          `json:"-"`
	Channel       string          `json:"-"`
	HeadCSV       string          `json:"-"`
	// PreviousVersion is the version of the entry below the head in its
	// release channel.
	PreviousVersion string `json:"-"`
}

// UnshippedBuild is an image of a component a release candidate leaves out,
// and the stream build holding it.
type UnshippedBuild struct {
	CandidateComponent
	Snapshot string `json:"snapshot"`
}

// CandidateCatalog is an FBC catalog image of a version, the Snapshot holding
// it and its ART build, NVR and OCP "" until ART resolves it: State "staged"
// for a catalog of an FBC Snapshot ART staged for stage, with the newest
// Release of that Snapshot, nil before one, and its ReleasePlan, or "built"
// for the newest catalog of an operator and OCP version that the FBC stream
// built for the version and ART did not stage, by the oldest stream build
// holding it.
type CandidateCatalog struct {
	State       string          `json:"state"`
	Snapshot    string          `json:"snapshot"`
	Component   string          `json:"component"`
	Pullspec    string          `json:"pullspec"`
	NVR         string          `json:"nvr"`
	OCP         string          `json:"ocp"`
	Release     *KonfluxRelease `json:"release"`
	ReleasePlan string          `json:"release_plan"`
}

// CandidateBuild is the version's STAGE build, with the version's prod
// Release. Its Snapshot and images are only checked against: the FBCs are the
// candidate.
type CandidateBuild struct {
	Snapshot   string               `json:"-"`
	Components []CandidateComponent `json:"-"`
	Prod       *KonfluxRelease      `json:"prod"`
}

// CIState tells whether an operator bundle reached the catalog CI installs
// from: State "in" with the stage Release that succeeded with it,
// "failed" with the one that failed, "none", or "unknown" for a build without
// a bundle or a stream without such Releases.
type CIState struct {
	State   string          `json:"state"`
	Release *KonfluxRelease `json:"release"`
}

// CIJob is a job's runs of a build, newest first.
type CIJob struct {
	JobName string  `json:"job_name"`
	Runs    []CIRun `json:"runs"`
}

// FBCBuild is an ART FBC build of a version with the periodic runs that
// installed from it, newest first. State is "shipped" once a prod Release
// holding it succeeded, else "candidate" for an FBC of the version's FBC
// candidate, else "staged" once a stage Release holding it succeeded, else "".
type FBCBuild struct {
	Operator string     `json:"operator"`
	OCP      string     `json:"ocp"`
	NVR      string     `json:"nvr"`
	Digest   string     `json:"digest"`
	BuiltAt  *time.Time `json:"built_at"`
	State    string     `json:"state"`
	Runs     []FBCRun   `json:"runs"`
}

type FBCRun struct {
	Job       string    `json:"job"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at"`
	ProwURL   string    `json:"prow_url"`
	SippyURL  string    `json:"sippy_url"`
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
	Image    string `json:"image"`
	NVR      string `json:"nvr"`
	BuildURL string `json:"build_url"`
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

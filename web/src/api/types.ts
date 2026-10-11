export interface KonfluxRelease {
	name: string;
	release_plan: string;
	released_status: string;
	released_reason: string;
	/** Task and step of the last managed pipeline attempt of a failed Release. */
	failed_task?: string;
	failed_step?: string;
	created_at: string;
	start_time?: string;
	completion_time?: string;
}

/**
 * What decides whether a version can ship. candidate is null with reason set
 * when there is none; a shipped version sets nothing else. ci is set with the
 * candidate. newest is set when the version's newest build holds, for a
 * component the candidate holds, another image than the candidate does and is
 * not ci's latest_in_ci, or when there is no candidate.
 */
export interface ReleaseCandidate {
	shipped: boolean;
	candidate: CandidateBuild | null;
	reason: string;
	newest: NewestBuild | null;
	ci: CandidateCI | null;
}

/** The release candidate: the version's STAGE build. */
export interface CandidateBuild {
	snapshot: string;
	created_at: string;
	components: CandidateComponent[];
	stage: StageFlag;
	/** The version's prod Release. */
	prod: KonfluxRelease | null;
}

/** The version's newest build and whether its operator bundle is in CI. */
export interface NewestBuild {
	snapshot: string;
	created_at: string;
	ci: CIState;
}

/**
 * Whether an operator bundle reached the catalog CI installs from, with the
 * per-build stage Release that decides it; "unknown" without a bundle or such
 * Releases.
 */
export interface CIState {
	state: "in" | "failed" | "none" | "unknown";
	release: KonfluxRelease | null;
}

/** A build named by the oldest stream Snapshot holding its operator bundle. */
export interface BuildLabel {
	snapshot: string;
	created_at: string;
}

/**
 * The periodic runs since the candidate's operator bundle was first built, by
 * the build each installed; the newest bundle of the version's builds in CI,
 * by its label, unless it is the candidate's; and whether the candidate needs
 * refreshing.
 */
export interface CandidateCI {
	since: string;
	builds: CIBuild[];
	latest_in_ci: BuildLabel | null;
	refresh: "refresh" | "needs_art" | "waiting" | "current";
}

/**
 * The runs that installed one build: the candidate, another build with its
 * label and CI state, an unknown build, or no Quay.
 */
export interface CIBuild {
	kind: "candidate" | "build" | "unknown" | "not_installed";
	/** "" unless candidate or build. */
	snapshot: string;
	created_at: string | null;
	ci: CIState | null;
	jobs: CIJob[];
}

/** A job's runs of a build, newest first. */
export interface CIJob {
	job_name: string;
	runs: CIRun[];
}

export interface CIRun {
	state: string;
	started_at: string;
	prow_url: string;
}

/** A build image; nvr and build_url are "" until ART build history has it. */
export interface CandidateComponent {
	name: string;
	image: string;
	nvr: string;
	build_url: string;
	/** null until the image scan sync has read it. */
	scan: ImageScan | null;
}

/** The CVE scan of an image by its build PipelineRun, whose Konflux page is url. */
export interface ImageScan {
	state: "pending" | "scanned" | "scan_failed" | "not_scanned";
	/** null unless scanned. */
	counts: ScanCounts | null;
	url: string;
}

/**
 * A scan's CVEs by severity, with and without a fix. scanner_arch_findings
 * counts a CVE once per architecture it is found in; unique_cves once.
 */
export interface ScanCounts {
	basis: "scanner_arch_findings" | "unique_cves";
	fixable: SeverityCounts;
	no_fix: SeverityCounts;
}

export interface SeverityCounts {
	critical: number;
	high: number;
	medium: number;
	low: number;
	unknown: number;
}

/**
 * Whether every image of a build is in a Snapshot a STAGE Release succeeded
 * with; "unknown" when the stream has no STAGE Release. The candidate is
 * always staged, when its own stage Release completed.
 */
export interface StageFlag {
	state: "staged" | "not_staged" | "unknown";
	staged_at: string;
}

/** An upstream commit in, or missing from, one component of a build. */
export interface BuildCommit {
	component: string;
	commit_sha: string;
	commit_url: string;
}

/** A ticket with the candidate's commits whose message names it. */
export interface BuildTicket {
	key: string;
	summary: string;
	status: string;
	priority: string;
	fix_version: string;
	assignee: string;
	issue_type: string;
	link: string;
	qa_contact: string;
	in_build: BuildCommit[];
	/**
	 * For a Target Version ticket no candidate commit names, the
	 * default-branch commits naming it that the candidate lacks.
	 */
	not_in_build: BuildCommit[];
}

/** A release's tickets checked against its candidate build. */
export interface BuildTickets {
	/** null when there is no candidate; reason says why ("shipped", ...). */
	build: { snapshot: string; source: "staged" | "newest" } | null;
	reason?: string;
	/** The previous version whose candidate the .z tickets are new since. */
	z_since?: string;
	/** Build components whose commits were not read. */
	not_compared: { component: string; reason: string }[];
	tickets: BuildTicket[];
}

export interface IssueSummary {
	total: number;
	verified: number;
	open: number;
	cves: number;
}

export interface ReleaseVersion {
	name: string;
	release_date?: string;
	released: boolean;
	release_ticket_key?: string;
	release_ticket_assignee?: string;
	konflux_application?: string;
	due_date?: string;
}

export interface ReadinessResponse {
	signal: "green" | "yellow" | "red";
	message: string;
	/** Released in Jira or published in the Red Hat catalog. */
	shipped: boolean;
}

export interface ReleaseOverview {
	release: ReleaseVersion;
	issue_summary?: IssueSummary;
	readiness: ReadinessResponse;
	// Newest snapshot in the version's Konflux applications, not bound to this version.
	latest_build?: string;
	shipped: boolean;
	next_in_stream: boolean;
}

export interface DashboardConfig {
	jira_base_url: string;
	jira_project: string;
	jira_enabled: boolean;
	/** "" disables Konflux links. */
	konflux_ui_url: string;
	konflux_namespace: string;
}

interface SyncProblem {
	source: string;
	message: string;
	since: string | null;
	last_success: string | null;
}

export interface SyncStatus {
	problems: SyncProblem[];
}

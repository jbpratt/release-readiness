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
 * What decides whether a version can ship. candidate is null with reason set,
 * and builds empty, when there is none; a shipped version sets nothing else.
 */
export interface ReleaseCandidate {
	shipped: boolean;
	candidate: CandidateBuild | null;
	reason: string;
	builds: BuildRow[];
}

/** The Snapshot up for release: the version's STAGE build, else its newest. */
export interface CandidateBuild {
	snapshot: string;
	source: "staged" | "newest";
	created_at: string;
	components: CandidateComponent[];
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
 * A build that matters to the version: the candidate, its newest build, and
 * when neither was tested, its newest build a periodic run tested.
 */
export interface BuildRow {
	snapshot: string;
	created_at: string;
	roles: ("candidate" | "newest" | "last_tested")[];
	stage: StageFlag;
	/** The version's prod Release, on the candidate only. */
	prod: KonfluxRelease | null;
	/** Per job, the periodic runs that tested the build. */
	ci: CIJob[];
}

/**
 * Whether every image of a build is in a Snapshot a STAGE Release succeeded
 * with; "unknown" when the stream has no STAGE Release.
 */
export interface StageFlag {
	state: "staged" | "not_staged" | "unknown";
	/** When the build reached stage, once staged. */
	staged_at: string | null;
	total: number;
	not_staged: string[];
	/** The newest STAGE Release that did not succeed holding a not-staged image. */
	release: KonfluxRelease | null;
	/** Not-staged images in no STAGE Release whose bundle in the build is staged. */
	no_bundle: string[];
	/** The other not-staged images in no STAGE Release. */
	no_release: string[];
}

/** A job's newest run of a build and how many of its runs tested it. */
export interface CIJob {
	job_name: string;
	state: string;
	prow_url: string;
	started_at: string | null;
	runs: number;
}

export interface JiraIssue {
	key: string;
	summary: string;
	status: string;
	priority: string;
	fix_version: string;
	assignee: string;
	issue_type: string;
	link: string;
	qa_contact: string;
}

/** An upstream commit in, or missing from, one component of a build. */
export interface BuildCommit {
	component: string;
	commit_sha: string;
	commit_url: string;
}

/** A ticket with the candidate's commits whose message names it. */
export interface BuildTicket extends JiraIssue {
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

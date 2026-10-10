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
 * when there is none; a shipped version sets nothing else.
 */
export interface ReleaseCandidate {
	shipped: boolean;
	candidate: CandidateBuild | null;
	reason: string;
	stage: StageFlag | null;
	prod: KonfluxRelease | null;
	ci: CandidateCI;
}

/** The Snapshot up for release: the version's STAGE build, else the stream's newest. */
export interface CandidateBuild {
	snapshot: string;
	source: "staged" | "newest";
	created_at: string;
	/** Its stage Release's completion time when source is "staged". */
	staged_at: string | null;
	components: CandidateComponent[];
}

/** A build image; nvr and build_url are "" until ART build history has it. */
export interface CandidateComponent {
	name: string;
	image: string;
	nvr: string;
	build_url: string;
}

/**
 * Whether every image of the stream's newest build is in a Snapshot a STAGE
 * Release succeeded with; "unknown" when the stream has no STAGE Release.
 */
export interface StageFlag {
	state: "staged" | "not_staged" | "unknown";
	snapshot: string;
	created_at: string;
	total: number;
	not_staged: string[];
	/** The newest STAGE Release that did not succeed holding a not-staged image. */
	release: KonfluxRelease | null;
}

/** The jobs whose runs tested the candidate; last_tested only when none did. */
export interface CandidateCI {
	jobs: CIJob[];
	last_tested: LastTested | null;
}

/** A job's newest run of a build and how many of its runs tested it. */
export interface CIJob {
	job_name: string;
	state: string;
	prow_url: string;
	started_at: string | null;
	runs: number;
}

/** The newest build a run tested, named by its operator bundle's NVR. */
export interface LastTested {
	bundle_nvr: string;
	tested_at: string | null;
	jobs: CIJob[];
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

/** An upstream commit in one component of a build. */
export interface BuildCommit {
	component: string;
	commit_sha: string;
	commit_url: string;
}

/** A ticket with the selected STAGE build's commits whose message names it. */
export interface BuildTicket extends JiraIssue {
	in_build: BuildCommit[];
}

/** A release's tickets checked against its selected STAGE build. */
export interface BuildTickets {
	/** null when no STAGE build is selected; reason says why. */
	build: { snapshot: string; completed_at: string } | null;
	reason?: string;
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

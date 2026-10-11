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
 * candidate, and without one when the version has builds. newest is set when
 * the version's newest build holds, for a component the candidate holds,
 * another image than the candidate does, or when there is no candidate, and is
 * not ci's passed nor, beside the candidate, its latest_in_ci.
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
 * The periodic runs since the candidate's operator bundle was first built, or
 * without a candidate since the first of the version's builds' bundles was, by
 * the build each installed; the newest bundle of the version's builds in CI,
 * by its label, unless it is the candidate's; passed, the newest of them,
 * newer than the candidate, that a run passed with, and fbc, its FBCs, null
 * until ART's record of each of the version's FBC images is read; and what to
 * do: with a candidate, rerun release-from-fbc with passed, wait on a newer
 * build in CI, or nothing (current); without one, pick passed, or nothing
 * (none).
 */
export interface CandidateCI {
	since: string;
	builds: CIBuild[];
	latest_in_ci: BuildLabel | null;
	passed: BuildLabel | null;
	fbc: BuildFBC | null;
	refresh: "rerun" | "waiting" | "current" | "pick" | "none";
}

/**
 * A build's FBCs for release-from-fbc: of the version's FBC images of each
 * operator and OCP version, the pullspec of the one ART built last that holds
 * a bundle of the build, and the pairs without one, each with ART build
 * history's search for the operator's FBCs ("" when ART links are off).
 */
export interface BuildFBC {
	pullspecs: string[];
	missing: { operator: string; ocp: string; url: string }[];
}

/**
 * The runs that installed one build, or failed to install it: the candidate,
 * another build with its label and CI state, or an unknown build. Without a
 * candidate, no row is the candidate's.
 */
export interface CIBuild {
	kind: "candidate" | "build" | "unknown";
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
	install_failed?: boolean;
}

/** A build image; nvr and build_url are "" until ART build history has it. */
export interface CandidateComponent {
	name: string;
	image: string;
	nvr: string;
	build_url: string;
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
	/** "", as is message, when there is nothing to flag. */
	signal: "green" | "yellow" | "red" | "";
	message: string;
	/** Released in Jira or published in the Red Hat catalog. */
	shipped: boolean;
}

export interface ReleaseOverview {
	release: ReleaseVersion;
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

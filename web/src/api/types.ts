export interface KonfluxRelease {
	name: string;
	released_status: string;
	released_reason: string;
	/** Task and step of the last managed pipeline attempt of a failed Release. */
	failed_task?: string;
	failed_step?: string;
	completion_time?: string;
}

/**
 * What decides whether a version can ship. candidate is null with reason set
 * when there is none. ci is set with the candidate, and without one when the
 * version has builds. newest is set when the version's newest build holds,
 * for a component the candidate holds, another image than the candidate does,
 * or when there is no candidate, and is not ci's passed nor, beside the
 * candidate, its latest_in_ci. A shipped version keeps its candidate and ci,
 * but has no reason, newest, or anything in ci to pick, re-run or wait on.
 */
export interface ReleaseCandidate {
	shipped: boolean;
	candidate: CandidateBuild | null;
	reason: string;
	newest: NewestBuild | null;
	ci: CandidateCI | null;
	/** The version's FBC catalogs, staged ones first. */
	catalogs: CandidateCatalog[] | null;
	/**
	 * With a candidate, the newest image of the version's builds of each
	 * component the candidate leaves out.
	 */
	unshipped_builds: UnshippedBuild[] | null;
	/**
	 * The version's operator FBCs up for release: as ART staged them for
	 * stage, else as its stream built them. Null while the version has no
	 * FBC built or staged.
	 */
	fbc_candidate: FBCCandidate | null;
	/** The version's operator FBCs as its stream built them. */
	fbc_stream: FBCCandidate | null;
}

/**
 * A version's operator FBCs, one per operator and OCP version slot, from the
 * newest staged FBC Snapshot ("stage_snapshots") or the newest successful ART
 * build ("stream"). operators always holds quay-operator,
 * container-security-operator and quay-bridge-operator; unknown holds the
 * staged catalogs no NVR names, which fill no slot. pullspecs, release-from-fbc's
 * fbc-pullspecs, is empty unless state is "complete"; installable is set when
 * every operator's head bundle reached stage through an fbc-ri Release.
 */
export interface FBCCandidate {
	source: "stage_snapshots" | "stream";
	state: "complete" | "incomplete" | "inconsistent" | "unknown";
	errors: string[];
	operators: OperatorFBC[];
	unknown: FBCMember[];
	pullspecs: string[];
	installable: boolean;
}

/**
 * One operator's FBCs, keyed by the bundle heading its newest FBC's release
 * channel ("" for none): members are on that bundle, conflicts are not.
 */
export interface OperatorFBC {
	operator: string;
	head_bundle_nvr: string;
	channel: string;
	head_csv: string;
	coverage: { members: number; slots: number };
	members: FBCMember[];
	missing_ocp: string[];
	conflicts: FBCMember[];
	/** Whether the head bundle reached stage through an fbc-ri Release. */
	fbc_ri_stage: CIState;
	image_check: ImageCheck;
}

/**
 * The head bundle against the image Snapshot's bundle of the operator: its
 * STAGE build, else its newest build. "absent" when either NVR is unknown.
 */
export interface ImageCheck {
	state: "matches" | "differs" | "absent";
	snapshot: string;
	bundle_nvr: string;
}

/**
 * Runs that installed from an FBC: exact from its digest, equivalent from
 * another FBC of its operator and OCP version on the same head bundle.
 */
export interface FBCCI {
	exact: CIJob[];
	equivalent: CIJob[];
}

/**
 * An FBC catalog image; nvr and head_bundle_nvr are "" until resolved. A
 * staged one has its FBC Snapshot and that Snapshot's newest Release.
 */
export interface FBCMember {
	ocp: string;
	digest: string;
	pullspec: string;
	nvr: string;
	build_url: string;
	head_bundle_nvr: string;
	snapshot: string;
	release: KonfluxRelease | null;
	/** Set on a slot's FBC; null on an unknown catalog. */
	ci: FBCCI | null;
}

/** An image of a component the candidate leaves out, and its stream build. */
export interface UnshippedBuild extends CandidateComponent {
	snapshot: string;
}

/**
 * An FBC catalog image of the version, the Snapshot holding it and its ART
 * build; nvr and ocp are "" until ART resolves it. A staged catalog is one of
 * an FBC Snapshot ART staged for stage, with that Snapshot's newest Release,
 * null before one, and its ReleasePlan; a built one is the newest catalog of
 * an operator and OCP version the FBC stream built and ART did not stage.
 */
export interface CandidateCatalog {
	state: "staged" | "built";
	snapshot: string;
	component: string;
	/** The catalog's pullspec, repo@sha256:... */
	pullspec: string;
	nvr: string;
	ocp: string;
	release: KonfluxRelease | null;
	release_plan: string;
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

/** The version's newest build, its images, and whether its operator bundle is in CI. */
export interface NewestBuild extends BuildLabel {
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

/**
 * A build named by the oldest stream Snapshot holding its operator bundle;
 * without a candidate, ci's passed has that build's images.
 */
export interface BuildLabel {
	snapshot: string;
	created_at: string;
	components?: CandidateComponent[];
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
	/** The image's pullspec, repo@sha256:... */
	image: string;
	nvr: string;
	build_url: string;
}

/** The candidate's stage: when its own stage Release completed. */
export interface StageFlag {
	staged_at: string;
}

export interface JiraIssue {
	key: string;
	summary: string;
	issue_type: string;
	link: string;
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
}

export interface ReleaseOverview {
	release: ReleaseVersion;
	readiness: ReadinessResponse;
	latest_build?: string;
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

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
 * What decides whether a version can ship: its operator FBCs. candidate is
 * the version's STAGE build, null with reason set when there is none; a
 * shipped version has no reason.
 */
export interface ReleaseCandidate {
	shipped: boolean;
	candidate: CandidateBuild | null;
	reason: string;
	/** The version's FBC catalogs, staged ones first. */
	catalogs: CandidateCatalog[] | null;
	/**
	 * With a STAGE build, the newest image of the version's builds of each
	 * component it leaves out.
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

/** The version's STAGE build. */
export interface CandidateBuild {
	/** The version's prod Release. */
	prod: KonfluxRelease | null;
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

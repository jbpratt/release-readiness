import {
	Breadcrumb,
	BreadcrumbHeading,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
	ClipboardCopy,
	clipboardCopyFunc,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	EmptyState,
	EmptyStateBody,
	Flex,
	FlexItem,
	Icon,
	Label,
	type LabelProps,
	PageSection,
	Popover,
	ProgressStep,
	type ProgressStepProps,
	ProgressStepper,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ExternalLinkAltIcon,
	InProgressIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { type ReactNode, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getCandidate, getRelease, listReleaseIssues } from "../api/client";
import type {
	BuildLabel,
	CandidateBuild,
	CandidateCI,
	CandidateComponent,
	CIJob,
	CIState,
	DashboardConfig,
	JiraIssue,
	NewestBuild,
	ReleaseCandidate,
	ReleaseVersion,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { days, due } from "../utils/format";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	RELEASE_FROM_FBC_URL,
	sippyJobUrl,
	ticketsJql,
} from "../utils/links";
import {
	ciStep,
	componentLabel,
	FBC_PLACEHOLDER,
	fbcCopy,
	fbcParams,
	jobLabels,
	newestCIStep,
	nvrLabel,
	releaseLine,
	runsStatus,
	type StatusLine,
	stepperBuild,
	tknCommand,
} from "../utils/releaseDetail";

/** A Konflux UI page of the version's application. */
type KonfluxLink = (
	kind?: "snapshots" | "releases",
	name?: string,
) => string | null;

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };

/** A local date and time to the minute: "Oct 9, 21:48". */
const when = (iso: string) =>
	new Date(iso).toLocaleString(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
		hourCycle: "h23",
	});

export default function ReleaseDetail() {
	const { version } = useParams<{ version: string }>();
	const config = useConfig();

	const { data: release, loading: loadingRelease } = useCachedFetch(
		`release:${version}`,
		() => getRelease(version!),
	);
	const { data: tickets, error: ticketsError } = useCachedFetch(
		`issues:${version}`,
		() => listReleaseIssues(version!),
	);
	const candidate = useCachedFetch(`candidate:${version}`, () =>
		getCandidate(version!),
	);

	if (loadingRelease && !release) {
		return (
			<PageSection>
				<div style={{ textAlign: "center" }}>
					<Spinner />
				</div>
			</PageSection>
		);
	}

	if (!release) {
		return (
			<PageSection>
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						Release not found
					</Title>
					<EmptyStateBody>
						No data found for release &quot;{version}&quot;.
					</EmptyStateBody>
				</EmptyState>
			</PageSection>
		);
	}

	const displayName = formatReleaseName(release.name);
	const app = release.konflux_application ?? "";
	const xyz = release.name.replace(/^[a-z]+-v/, "");
	// Every Release the candidate response names is in the version's application.
	const konflux: KonfluxLink = (kind, name) =>
		konfluxUrl(
			config?.konflux_ui_url ?? "",
			config?.konflux_namespace ?? "",
			app,
			kind,
			name,
		);

	return (
		<PageSection>
			<Breadcrumb style={{ marginBottom: "1rem" }}>
				<BreadcrumbItem>
					<Link to="/">Releases</Link>
				</BreadcrumbItem>
				<BreadcrumbHeading>{displayName}</BreadcrumbHeading>
			</Breadcrumb>

			<ReleaseStatusCard
				release={release}
				shipped={candidate.data?.shipped ?? release.released}
				jiraBaseUrl={config?.jira_base_url}
			/>

			<BuildsCard
				data={candidate.data}
				error={candidate.error}
				version={xyz}
				app={app}
				konflux={konflux}
				bugs={
					config?.jira_enabled === false
						? undefined
						: tickets?.map((t) => t.key)
				}
				dueDate={release.due_date}
			/>

			{candidate.data?.ci && candidate.data.ci.builds.length > 0 && (
				<CICard ci={candidate.data.ci} version={xyz} konflux={konflux} />
			)}

			<TicketsCard
				issues={tickets}
				error={ticketsError}
				version={version!}
				config={config}
			/>
		</PageSection>
	);
}

/** The target date, flagged while unshipped once past or within 3 days, the ticket and its assignee. */
function ReleaseStatusCard({
	release,
	shipped,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	shipped: boolean;
	jiraBaseUrl?: string;
}) {
	const ticket = release.release_ticket_key;
	const target = release.due_date ?? release.release_date;
	const dueDate = target ? due(target, shipped) : undefined;
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Release Status</CardTitle>
			<CardBody>
				<Flex justifyContent={{ default: "justifyContentSpaceEvenly" }}>
					<FlexItem style={{ textAlign: "center" }}>
						<div className="rr-label">Target</div>
						<div>
							{dueDate?.date ?? "TBD"}
							{dueDate?.kind === "past" && (
								<Label
									color="red"
									icon={<ExclamationCircleIcon />}
									isCompact
									style={{ marginLeft: "0.5rem" }}
								>
									Past due date
								</Label>
							)}
							{dueDate?.kind === "soon" && (
								<Label
									color="yellow"
									icon={<ExclamationTriangleIcon />}
									isCompact
									style={{ marginLeft: "0.5rem" }}
								>
									{dueDate.days === 0
										? "Due today"
										: `Due in ${days(dueDate.days)}`}
								</Label>
							)}
						</div>
					</FlexItem>
					{ticket && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Ticket</div>
							<a
								href={jiraIssueUrl(
									ticket,
									jiraBaseUrl || "https://redhat.atlassian.net",
								)}
								{...external}
							>
								{ticket}
							</a>
						</FlexItem>
					)}
					<FlexItem style={{ textAlign: "center" }}>
						<div className="rr-label">Assignee</div>
						<div>{release.release_ticket_assignee || "Unassigned"}</div>
					</FlexItem>
				</Flex>
			</CardBody>
		</Card>
	);
}

/** A link opening in a new tab, or plain text without an href. */
function ExtLink({
	href,
	title,
	children,
}: {
	href: string | null;
	title?: string;
	children: ReactNode;
}) {
	return href ? (
		<a href={href} title={title} {...external}>
			{children}
		</a>
	) : (
		<span title={title}>{children}</span>
	);
}

/** A card title with a link out on the right, hidden without an href. */
function LinkedCardTitle({
	href,
	link,
	children,
}: {
	href: string | null;
	link: string;
	children: ReactNode;
}) {
	return (
		<CardTitle>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsCenter" }}
			>
				<FlexItem>{children}</FlexItem>
				{href && (
					<FlexItem>
						<Button
							component="a"
							variant="link"
							isInline
							href={href}
							{...external}
							icon={<ExternalLinkAltIcon />}
							iconPosition="end"
						>
							{link}
						</Button>
					</FlexItem>
				)}
			</Flex>
		</CardTitle>
	);
}

/** Tooltip content, one line each, without the empty ones. */
const lines = (...ls: (string | undefined)[]) =>
	ls.filter(Boolean).map((l) => <div key={l}>{l}</div>);

/**
 * The version's release candidate, its STAGE build, or why it has none and
 * the build to pick, with the build's images; a shipped version's is labeled,
 * with nothing to pick or release.
 */
function BuildsCard({
	data,
	error,
	version,
	app,
	konflux,
	bugs,
	dueDate,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	/** The version's X.Y.Z. */
	version: string;
	app: string;
	konflux: KonfluxLink;
	/** The version's ticket keys; undefined while unknown. */
	bugs?: string[];
	dueDate?: string;
}) {
	const c = data?.candidate;
	const anchor = data && stepperBuild(data.newest, data.ci);
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={konflux()} link="Konflux">
				{c ? `${version} release candidate` : "Release candidate"}
				{data?.shipped && (
					<Label color="green" isCompact style={{ marginLeft: "0.5rem" }}>
						Shipped
					</Label>
				)}
			</LinkedCardTitle>
			<CardBody>
				{error ? (
					error.message
				) : !data ? (
					<Spinner size="md" />
				) : c && data.ci ? (
					<Candidate
						candidate={c}
						ci={data.ci}
						newest={data.newest}
						app={app}
						konflux={konflux}
						fbc={
							!data.shipped && (
								<ReleaseFromFBC
									params={fbcParams(
										app,
										version,
										fbcCopy(data.ci),
										bugs ?? [],
										dueDate,
									)}
									bugsUnknown={!bugs}
								/>
							)
						}
					/>
				) : data.shipped ? (
					<NewestStepper
						newest={anchor}
						ci={data.ci}
						konflux={konflux}
						shipped
					/>
				) : (
					<>
						{`${data.reason.charAt(0).toUpperCase()}${data.reason.slice(1)}.`}
						{anchor && (
							<NewestStepper newest={anchor} ci={data.ci} konflux={konflux} />
						)}
						{data.ci?.refresh === "pick" && data.ci.passed && (
							<div style={line}>
								<Status status="warning">
									Pick the candidate: the{" "}
									<BuildLink build={data.ci.passed} konflux={konflux} /> build
									passed CI. <PassedCopy ci={data.ci} passed={data.ci.passed} />
								</Status>
							</div>
						)}
						{anchor?.components && (
							<ComponentTable components={anchor.components} app={app} />
						)}
					</>
				)}
			</CardBody>
		</Card>
	);
}

/**
 * The candidate: where it is, from its build through CI and stage to prod,
 * and its images. The CI step's popover has a newer build to re-run
 * release-from-fbc with or wait on, and the newest build; unshipped, the
 * Stage step's has release-from-fbc's params; the Prod step's, its Release.
 */
function Candidate({
	candidate,
	ci,
	newest,
	app,
	konflux,
	fbc,
}: {
	candidate: CandidateBuild;
	ci: CandidateCI;
	newest: NewestBuild | null;
	app: string;
	konflux: KonfluxLink;
	fbc?: ReactNode;
}) {
	const prod = candidate.prod && {
		name: candidate.prod.name,
		...releaseLine(candidate.prod),
	};
	const tested = ciStep(ci);
	const next =
		ci.refresh === "rerun" && ci.passed
			? " · re-run available"
			: ci.refresh === "waiting"
				? " · newer build waiting"
				: "";
	const latest = ci.latest_in_ci && (
		<BuildLink build={ci.latest_in_ci} konflux={konflux} />
	);
	return (
		<>
			<ProgressStepper
				isCenterAligned
				aria-label="Where the release candidate is"
			>
				<Step
					title="Build"
					variant="success"
					description={<BuildLink build={candidate} konflux={konflux} />}
				/>
				<Step
					title="CI"
					variant={tested.variant}
					description={tested.text + next}
					popoverRender={(ref) => (
						<Popover
							aria-label="CI detail"
							headerContent="CI"
							triggerRef={ref}
							minWidth="32rem"
							bodyContent={
								<>
									{ci.refresh === "rerun" && ci.passed ? (
										<Status status="warning">
											Re-run release-from-fbc with the{" "}
											<BuildLink build={ci.passed} konflux={konflux} /> build,
											which is newer and passed CI (optional).
											{candidate.prod &&
												" ART may refuse it after a prod attempt."}{" "}
											<PassedCopy ci={ci} passed={ci.passed} />
										</Status>
									) : ci.refresh === "waiting" ? (
										<Status status="running">
											Newer build {latest} is waiting on CI.
										</Status>
									) : (
										<Status status="done">Candidate is current.</Status>
									)}
									{newest && <NewestLine newest={newest} konflux={konflux} />}
								</>
							}
						/>
					)}
				/>
				<Step
					title="Stage"
					variant="success"
					isCurrent={!prod}
					description={`Pinned ${new Date(candidate.stage.staged_at).toLocaleDateString("en-US", { month: "short", day: "numeric" })}`}
					popoverRender={
						fbc
							? (ref) => (
									<Popover
										aria-label="Release from FBC"
										triggerRef={ref}
										minWidth="40rem"
										bodyContent={fbc}
									/>
								)
							: undefined
					}
				/>
				<Step
					title="Prod"
					variant={prod ? releaseVariants[prod.status] : "pending"}
					isCurrent={!!prod}
					description={
						prod ? (
							<ExtLink href={konflux("releases", prod.name)} title={prod.name}>
								{prod.status === "failed" ? "Release failed" : prod.text}
							</ExtLink>
						) : (
							"Not started"
						)
					}
					popoverRender={
						prod?.status === "failed"
							? (ref) => (
									<Popover
										aria-label="Prod detail"
										headerContent="Prod"
										triggerRef={ref}
										bodyContent={
											<ExtLink
												href={konflux("releases", prod.name)}
												title={prod.name}
											>
												{prod.text}
											</ExtLink>
										}
									/>
								)
							: undefined
					}
				/>
			</ProgressStepper>
			<ComponentTable components={candidate.components} app={app} />
		</>
	);
}

/**
 * release-from-fbc's params, a dry-run tkn command and the Pipeline's console
 * page, to review and run by hand: RR starts no run.
 */
function ReleaseFromFBC({
	params,
	bugsUnknown,
}: {
	params: Record<string, string>;
	/** The tickets did not load, so jira-bugs is unknown, not empty. */
	bugsUnknown: boolean;
}) {
	const pullspecs = params["fbc-pullspecs"];
	const row = (term: string, value: ReactNode, hint?: string) => (
		<DescriptionListGroup>
			<DescriptionListTerm>{term}</DescriptionListTerm>
			<DescriptionListDescription>
				{value} {hint && <span style={muted}>({hint})</span>}
			</DescriptionListDescription>
		</DescriptionListGroup>
	);
	return (
		<>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsCenter" }}
			>
				<Title headingLevel="h4" size="md">
					Release from FBC
				</Title>
				<Button
					component="a"
					variant="link"
					isInline
					href={RELEASE_FROM_FBC_URL}
					{...external}
					icon={<ExternalLinkAltIcon />}
					iconPosition="end"
				>
					Pipeline
				</Button>
			</Flex>
			<DescriptionList
				isHorizontal
				isCompact
				horizontalTermWidthModifier={{ default: "12rem" }}
				style={line}
			>
				{row("group", params.group)}
				{row("assembly", params.assembly)}
				{row(
					"fbc-pullspecs",
					pullspecs === FBC_PLACEHOLDER ? (
						<span style={muted}>not available yet</span>
					) : (
						<ClipboardCopy variant="inline-compact" truncation>
							{pullspecs}
						</ClipboardCopy>
					),
				)}
				{row(
					"jira-bugs",
					params["jira-bugs"] ?? (bugsUnknown ? "unavailable" : "none"),
					"review before running",
				)}
				{row(
					"target-release-date",
					params["target-release-date"] ?? "none",
					"hint",
				)}
			</DescriptionList>
			<ClipboardCopy variant="inline-compact" isCode isBlock style={line}>
				{tknCommand(params)}
			</ClipboardCopy>
		</>
	);
}

/** An image's digest to 12 hex digits: "sha256:0123456789ab". */
const shortDigest = (image: string) =>
	image.slice(image.indexOf("@") + 1).slice(0, 19);

/**
 * A build's images, each by its ART build, else by its short digest, copying
 * its pullspec.
 */
function ComponentTable({
	components,
	app,
}: {
	components: CandidateComponent[];
	app: string;
}) {
	return (
		<Table variant="compact" style={{ marginTop: "1rem" }}>
			<Thead>
				<Tr>
					<Th>Component</Th>
					<Th>Build</Th>
				</Tr>
			</Thead>
			<Tbody>
				{components.map((c) => (
					<Tr key={c.name}>
						<Td>{componentLabel(c.name, app)}</Td>
						<Td>
							{c.nvr ? (
								<ExtLink href={c.build_url || null} title={c.nvr}>
									{nvrLabel(c.nvr)}
								</ExtLink>
							) : (
								<span title={c.image}>
									<ClipboardCopy
										variant="inline-compact"
										isCode
										onCopy={(e) => clipboardCopyFunc(e, c.image)}
									>
										{shortDigest(c.image)}
									</ClipboardCopy>{" "}
									<span style={muted}>ART NVR not resolved</span>
								</span>
							)}
						</Td>
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}

/**
 * Without a candidate, where the version's newest build is: built, its CI,
 * and not yet staged or released. The CI step links the Release that decides
 * whether its bundle reached CI. A shipped version without a candidate has
 * no stage data and may have no build, but is in prod.
 */
function NewestStepper({
	newest,
	ci,
	konflux,
	shipped,
}: {
	newest?: NewestBuild | null;
	ci: CandidateCI | null;
	konflux: KonfluxLink;
	shipped?: boolean;
}) {
	const tested = newest && newestCIStep(newest, ci);
	const r = newest?.ci.release;
	return (
		<ProgressStepper
			isCenterAligned
			aria-label="Where the newest build is"
			style={{ marginTop: "1rem" }}
		>
			{newest && tested ? (
				<>
					<Step
						title="Build"
						variant="success"
						description={<BuildLink build={newest} konflux={konflux} />}
					/>
					<Step
						title="CI"
						variant={tested.variant}
						isCurrent={!shipped}
						description={
							r ? (
								<ExtLink href={konflux("releases", r.name)} title={r.name}>
									{tested.text}
								</ExtLink>
							) : (
								tested.text
							)
						}
					/>
				</>
			) : (
				<>
					<Step title="Build" variant="pending" description="No data" />
					<Step title="CI" variant="pending" description="No data" />
				</>
			)}
			<Step
				title="Stage"
				variant="pending"
				description={shipped ? "No data" : "Not staged"}
			/>
			<Step
				title="Prod"
				variant={shipped ? "success" : "pending"}
				isCurrent={shipped}
				description={shipped ? "Shipped" : "Not released"}
			/>
		</ProgressStepper>
	);
}

const line = { marginTop: "0.5rem" };

/**
 * What to copy to run release-from-fbc with ci's passed build: its FBC
 * pullspecs, once it has one for every operator and OCP version, else its
 * Snapshot; then the pairs it has no FBC of, each linked to ART's search.
 */
function PassedCopy({ ci, passed }: { ci: CandidateCI; passed: BuildLabel }) {
	const copy = fbcCopy(ci);
	const missing = ci.fbc?.missing ?? [];
	return (
		<>
			{copy ? (
				<>
					{`FBC pullspecs (${ci.fbc?.pullspecs.length}):`}{" "}
					<ClipboardCopy variant="inline-compact" truncation>
						{copy}
					</ClipboardCopy>
				</>
			) : (
				<ClipboardCopy variant="inline-compact">
					{passed.snapshot}
				</ClipboardCopy>
			)}
			{missing.length > 0 && (
				<div style={muted}>
					Missing FBC:{" "}
					{missing.map((m, i) => (
						<span key={`${m.operator} ${m.ocp}`}>
							{i > 0 && ", "}
							<ExtLink href={m.url || null}>
								{`${m.operator} OCP ${m.ocp}`}
							</ExtLink>
						</span>
					))}
					.
				</div>
			)}
		</>
	);
}

/** A build's date and time, linked to its Snapshot. */
function BuildLink({
	build,
	konflux,
}: {
	build: BuildLabel;
	konflux: KonfluxLink;
}) {
	return (
		<ExtLink href={konflux("snapshots", build.snapshot)} title={build.snapshot}>
			{when(build.created_at)}
		</ExtLink>
	);
}

/** Whether a build is in CI, linked to the Release that decides it; nothing when unknown. */
function CIStateText({ ci, konflux }: { ci: CIState; konflux: KonfluxLink }) {
	const r = ci.release;
	if (ci.state === "none") return "not in CI";
	if (!r) return null;
	return (
		<ExtLink href={konflux("releases", r.name)} title={r.name}>
			{ci.state === "in"
				? "in CI"
				: releaseLine(r).text.replace(/^Release failed/, "failed to reach CI")}
		</ExtLink>
	);
}

/** The version's newest build: its date and time, linked to its Snapshot, and whether it is in CI. */
function NewestLine({
	newest,
	konflux,
}: {
	newest: NewestBuild;
	konflux: KonfluxLink;
}) {
	return (
		<div style={line}>
			Newest build <BuildLink build={newest} konflux={konflux} />, not pinned
			{newest.ci.state !== "unknown" && (
				<>
					{" · "}
					<CIStateText ci={newest.ci} konflux={konflux} />
				</>
			)}
		</div>
	);
}

// What a step's variant means, read out after its title: only its icon shows it.
const stepStates = {
	success: "done",
	warning: "warning",
	danger: "failed",
	info: "in progress",
	pending: "not started",
} as const;

const releaseVariants: Record<StatusLine["status"], keyof typeof stepStates> = {
	done: "success",
	failed: "danger",
	running: "info",
	warning: "warning",
};

/** A step of the candidate's stepper, read out as "<title>: <state>". */
function Step({
	title,
	variant,
	...props
}: Pick<ProgressStepProps, "isCurrent" | "description" | "popoverRender"> & {
	title: string;
	variant: keyof typeof stepStates;
}) {
	const id = `candidate-${title.toLowerCase()}`;
	return (
		<ProgressStep
			id={id}
			titleId={`${id}-title`}
			variant={variant}
			aria-label={`${title}: ${stepStates[variant]}`}
			{...props}
		>
			{title}
		</ProgressStep>
	);
}

const statusIcons: Record<StatusLine["status"], ReactNode> = {
	done: (
		<Icon status="success" isInline>
			<CheckCircleIcon />
		</Icon>
	),
	failed: (
		<Icon status="danger" isInline>
			<ExclamationCircleIcon />
		</Icon>
	),
	running: (
		<Icon status="info" isInline>
			<InProgressIcon />
		</Icon>
	),
	warning: (
		<Icon status="warning" isInline>
			<ExclamationTriangleIcon />
		</Icon>
	),
};

/** A status icon, then its text, which wraps clear of it. */
function Status({
	status,
	children,
}: {
	status: StatusLine["status"];
	children: ReactNode;
}) {
	return (
		<Flex gap={{ default: "gapSm" }} flexWrap={{ default: "nowrap" }}>
			<FlexItem>{statusIcons[status]}</FlexItem>
			<FlexItem>{children}</FlexItem>
		</Flex>
	);
}

// Prow job states as the OCP release controller words them.
const ciStates: Record<string, string> = {
	success: "Succeeded",
	failure: "Failed",
	error: "Error",
};

const runState = (state: string) =>
	ciStates[state] ?? state.charAt(0).toUpperCase() + state.slice(1);

const nowrap = { whiteSpace: "nowrap" } as const;

// A build's date and time in ms; 0, so oldest, for the unknown build.
const time = (iso: string | null) => (iso ? Date.parse(iso) : 0);

/**
 * The periodic runs since the candidate's bundle was first built, or the
 * version's builds' without one, after the OCP release controller: a row per
 * build they installed, or failed to install, newest first, the unknown build
 * last.
 */
function CICard({
	ci,
	version,
	konflux,
}: {
	ci: CandidateCI;
	/** The version's X.Y.Z. */
	version: string;
	konflux: KonfluxLink;
}) {
	const jobs = [
		...new Set(ci.builds.flatMap((b) => b.jobs.map((j) => j.job_name))),
	];
	const labels = jobLabels(jobs);
	const label = (job: string) => labels[jobs.indexOf(job)];
	const candidate = ci.builds.find((b) => b.kind === "candidate");
	const notPinned = candidate?.created_at
		? `Not pinned: the ${version} release candidate is the ${when(candidate.created_at)} build.`
		: undefined;
	const builds = [...ci.builds].sort(
		(a, b) => time(b.created_at) - time(a.created_at),
	);
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>CI</CardTitle>
			<CardBody>
				<Table variant="compact" aria-label="CI runs by build">
					<Thead>
						<Tr>
							<Th modifier="fitContent">Build</Th>
							<Th modifier="fitContent">State</Th>
							<Th>Jobs (failures first)</Th>
						</Tr>
					</Thead>
					<Tbody>
						{builds.map((b) => (
							<Tr key={`${b.kind} ${b.snapshot}`}>
								<Td style={nowrap}>
									{b.created_at ? (
										<BuildLink
											build={{ snapshot: b.snapshot, created_at: b.created_at }}
											konflux={konflux}
										/>
									) : (
										<span style={muted}>Unknown build</span>
									)}
								</Td>
								<Td style={nowrap}>
									{b.kind === "candidate" ? (
										<Pill color="blue" tip={`Pinned to ship as ${version}.`}>
											release candidate
										</Pill>
									) : (
										b.ci && (
											<CIPill
												ci={b.ci}
												notPinned={notPinned}
												konflux={konflux}
											/>
										)
									)}
								</Td>
								<Td>
									{b.jobs.length > 0 ? (
										<JobPills jobs={b.jobs} label={label} version={version} />
									) : (
										<span style={muted}>No runs</span>
									)}
								</Td>
							</Tr>
						))}
					</Tbody>
				</Table>
			</CardBody>
		</Card>
	);
}

/**
 * A build's CI state as a pill, its meaning and notPinned on hover; in and
 * failed open the stage Release that decides it. Nothing when unknown.
 */
function CIPill({
	ci,
	notPinned,
	konflux,
}: {
	ci: CIState;
	/** Which build is pinned instead, when one is. */
	notPinned?: string;
	konflux: KonfluxLink;
}) {
	const r = ci.release;
	const href = r && konflux("releases", r.name);
	switch (ci.state) {
		case "in":
			return (
				<Pill
					color="teal"
					href={href}
					tip={lines("Reached the catalog CI installs from.", notPinned)}
				>
					in CI
				</Pill>
			);
		case "failed":
			return (
				<Pill
					color="red"
					href={href}
					tip={lines(r ? releaseLine(r).text : undefined, notPinned)}
				>
					failed to reach CI
				</Pill>
			);
		case "none":
			return (
				<Pill
					color="grey"
					tip={lines(
						"No stage Release put it in the catalog CI installs from.",
						notPinned,
					)}
				>
					not in CI
				</Pill>
			);
		default:
			return null;
	}
}

// Job statuses, failures first.
const jobRank = { danger: 0, warning: 1, success: 2, none: 3 } as const;

const jobStatus = (j: CIJob): keyof typeof jobRank =>
	runsStatus(j.runs.map((r) => r.state)) ?? "none";

/**
 * A pill per job, failures first, opening its Sippy page; its runs, each
 * opening it in Prow, on hover. Passing jobs fold into one "N passed" toggle.
 */
function JobPills({
	jobs,
	label,
	version,
}: {
	jobs: CIJob[];
	label: (job: string) => string;
	/** The version's X.Y.Z. */
	version: string;
}) {
	const [showPassed, setShowPassed] = useState(false);
	const sorted = [...jobs].sort(
		(a, b) =>
			jobRank[jobStatus(a)] - jobRank[jobStatus(b)] ||
			label(a.job_name).localeCompare(label(b.job_name)),
	);
	const passed = sorted.filter((j) => jobStatus(j) === "success");
	const pill = (job: CIJob) => {
		const s = jobStatus(job);
		return (
			<Pill
				key={job.job_name}
				status={s === "none" ? undefined : s}
				href={sippyJobUrl(version, job.job_name)}
				popover
				tip={
					<>
						<div>{job.job_name}</div>
						{job.runs.map((r) => (
							<div key={r.prow_url}>
								<ExtLink href={r.prow_url}>{when(r.started_at)}</ExtLink>
								{` · ${r.install_failed ? "Install failed" : runState(r.state)}`}
							</div>
						))}
					</>
				}
			>
				{label(job.job_name)}
				{job.runs.length > 1 && ` ×${job.runs.length}`}
			</Pill>
		);
	};
	return (
		<Flex gap={{ default: "gapXs" }}>
			{sorted.filter((j) => jobStatus(j) !== "success").map(pill)}
			{passed.length > 0 && (
				<Label
					isCompact
					isClickable
					status="success"
					render={({ className, content, componentRef }) => (
						<button
							type="button"
							className={className}
							ref={componentRef}
							aria-expanded={showPassed}
							onClick={() => setShowPassed(!showPassed)}
						>
							{content}
						</button>
					)}
				>
					{`${passed.length} passed`}
				</Label>
			)}
			{showPassed && passed.map(pill)}
		</Flex>
	);
}

/**
 * A compact label with a tooltip, or popover, opening href in a new tab;
 * without href it takes keyboard focus, which shows the tooltip.
 */
function Pill({
	tip,
	href,
	popover,
	children,
	...props
}: Pick<LabelProps, "color" | "status" | "children"> & {
	tip: ReactNode;
	href?: string | null;
	/** Whether tip has links, so shows in a popover that stays open under the pointer. */
	popover?: boolean;
}) {
	const [open, setOpen] = useState<"hover" | "focus">();
	const label = href ? (
		<Label
			isCompact
			isClickable
			render={({ className, content, componentRef }) => (
				<a className={className} href={href} ref={componentRef} {...external}>
					{content}
				</a>
			)}
			{...props}
		>
			{children}
		</Label>
	) : (
		<Label isCompact tabIndex={0} {...props}>
			{children}
		</Label>
	);
	return popover ? (
		// Open while focus is inside, and inline then so Tab reaches tip's links;
		// on hover it stays in body, where the card's overflow cannot flip it.
		// biome-ignore lint/a11y/noStaticElementInteractions: tracks focus, not input
		<span
			style={{ display: "contents" }}
			onFocus={(e) => {
				// React bubbles focus out of the hover portal; going inline then
				// would remount the link under a pending click.
				if (e.currentTarget.contains(e.target)) setOpen("focus");
			}}
			onBlur={(e) => {
				if (!e.currentTarget.contains(e.relatedTarget)) setOpen(undefined);
			}}
		>
			<Popover
				triggerAction="hover"
				isVisible={!!open}
				shouldOpen={() => setOpen((o) => o ?? "hover")}
				shouldClose={() => setOpen(undefined)}
				appendTo={open === "focus" ? "inline" : undefined}
				aria-label="Runs"
				bodyContent={tip}
			>
				{label}
			</Popover>
		</span>
	) : (
		<Tooltip content={tip}>{label}</Tooltip>
	);
}

function TicketsCard({
	issues,
	error,
	version,
	config,
}: {
	issues?: JiraIssue[];
	error?: Error;
	version: string;
	config?: DashboardConfig;
}) {
	const hasIssues = (issues ?? []).length > 0;
	const jira = config?.jira_project
		? jiraSearchUrl(
				ticketsJql(config.jira_project, version),
				config.jira_base_url,
			)
		: null;

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={jira} link="Jira">
				Tickets
				{hasIssues && ` (${issues!.length})`}
			</LinkedCardTitle>
			<CardBody>
				{hasIssues ? (
					<IssuesTable issues={issues!} />
				) : error ? (
					error.message
				) : !issues ? (
					<Spinner size="md" />
				) : config?.jira_enabled === false ? (
					"JIRA sync is not configured on this server, so linked tickets are not shown."
				) : (
					"No tickets have this Target Version."
				)}
			</CardBody>
		</Card>
	);
}

function IssuesTable({ issues }: { issues: JiraIssue[] }) {
	// Type, the second column, is the only sortable one.
	const [direction, setDirection] = useState<"asc" | "desc">();

	const sortedIssues = useMemo(() => {
		if (!direction) return issues;
		return [...issues].sort((a, b) => {
			const cmp = a.issue_type.localeCompare(b.issue_type);
			return direction === "asc" ? cmp : -cmp;
		});
	}, [issues, direction]);

	return (
		<Table variant="compact" style={{ tableLayout: "auto" }}>
			<Thead>
				<Tr>
					<Th style={{ whiteSpace: "nowrap" }}>Key</Th>
					<Th
						sort={{
							sortBy: { index: direction && 1, direction },
							onSort: (_event, _index, d) => setDirection(d),
							columnIndex: 1,
						}}
						modifier="fitContent"
					>
						Type
					</Th>
					<Th>Summary</Th>
				</Tr>
			</Thead>
			<Tbody>
				{sortedIssues.map((issue) => (
					<Tr key={issue.key}>
						<Td style={{ whiteSpace: "nowrap" }}>
							<a href={issue.link} {...external}>
								{issue.key}
							</a>
						</Td>
						<Td style={{ whiteSpace: "nowrap" }}>{issue.issue_type}</Td>
						<Td style={{ whiteSpace: "normal", wordBreak: "break-word" }}>
							{issue.summary}
						</Td>
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}

import {
	Breadcrumb,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
	ClipboardCopy,
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
import {
	Table,
	Tbody,
	Td,
	Th,
	Thead,
	type ThProps,
	Tr,
} from "@patternfly/react-table";
import { type ReactNode, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getBuildTickets, getCandidate, getRelease } from "../api/client";
import type {
	BuildCommit,
	BuildLabel,
	BuildTicket,
	BuildTickets,
	CandidateBuild,
	CandidateCI,
	CIJob,
	CIState,
	DashboardConfig,
	ImageScan,
	NewestBuild,
	ReleaseCandidate,
	ReleaseVersion,
	ScanCounts,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { days, due } from "../utils/format";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	ticketsJql,
} from "../utils/links";
import {
	BASES,
	ciWarning,
	componentLabel,
	fixableLabels,
	jobLabels,
	missingCommits,
	nvrLabel,
	releaseLine,
	SEVERITIES,
	SEVERITY_COLORS,
	type StatusLine,
	scanCell,
	statusIcon,
} from "../utils/releaseDetail";

/** A Konflux UI page of the version's application. */
type KonfluxLink = (
	kind?: "snapshots" | "releases",
	name?: string,
) => string | null;

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };
const textColor = {
	orange: "var(--pf-t--global--text--color--status--warning--default)",
	grey: muted.color,
};

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
		`buildTickets:${version}`,
		() => getBuildTickets(version!),
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
				<BreadcrumbItem isActive>{displayName}</BreadcrumbItem>
			</Breadcrumb>
			<Title headingLevel="h1" style={{ marginBottom: "1rem" }}>
				{displayName}
			</Title>

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
			/>

			{candidate.data?.ci && candidate.data.ci.builds.length > 0 && (
				<CICard ci={candidate.data.ci} version={xyz} konflux={konflux} />
			)}

			<TicketsCard
				data={tickets}
				error={ticketsError}
				version={version!}
				app={app}
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

const noNvr = (
	<span title="Not in ART build history yet" style={muted}>
		-
	</span>
);

/** Tooltip content, one line each, without the empty ones. */
const lines = (...ls: (string | undefined)[]) =>
	ls.filter(Boolean).map((l) => <div key={l}>{l}</div>);

/**
 * The version's release candidate, its STAGE build, or why it has none and
 * the build to pick.
 */
function BuildsCard({
	data,
	error,
	version,
	app,
	konflux,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	/** The version's X.Y.Z. */
	version: string;
	app: string;
	konflux: KonfluxLink;
}) {
	const c = data?.candidate;
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={konflux()} link="Konflux">
				{c ? (
					<>
						{version} release candidate ·{" "}
						<BuildLink build={c} konflux={konflux} />
					</>
				) : (
					"Release candidate"
				)}
			</LinkedCardTitle>
			<CardBody>
				{error ? (
					error.message
				) : !data ? (
					<Spinner size="md" />
				) : data.shipped ? (
					"Shipped."
				) : c && data.ci ? (
					<Candidate
						candidate={c}
						ci={data.ci}
						newest={data.newest}
						version={version}
						app={app}
						konflux={konflux}
					/>
				) : (
					<>
						{`${data.reason.charAt(0).toUpperCase()}${data.reason.slice(1)}.`}
						{data.ci?.refresh === "pick" && data.ci.passed && (
							<div style={line}>
								<Status status="warning">
									Pick the candidate: the{" "}
									<BuildLink build={data.ci.passed} konflux={konflux} /> build
									passed CI.{" "}
									<ClipboardCopy variant="inline-compact">
										{data.ci.passed.snapshot}
									</ClipboardCopy>
								</Status>
							</div>
						)}
						{data.newest && (
							<NewestLine newest={data.newest} konflux={konflux} />
						)}
					</>
				)}
			</CardBody>
		</Card>
	);
}

/**
 * The candidate: whether it is in stage and in prod, whether CI tested it, a
 * newer build to re-run release-from-fbc with or wait on, the newest build,
 * and its images.
 */
function Candidate({
	candidate,
	ci,
	newest,
	version,
	app,
	konflux,
}: {
	candidate: CandidateBuild;
	ci: CandidateCI;
	newest: NewestBuild | null;
	version: string;
	app: string;
	konflux: KonfluxLink;
}) {
	const hasScans = candidate.components.some((c) => c.scan);
	const prod = candidate.prod && {
		name: candidate.prod.name,
		...releaseLine(candidate.prod),
	};
	const warning = ciWarning(ci);
	const latest = ci.latest_in_ci && (
		<BuildLink build={ci.latest_in_ci} konflux={konflux} />
	);
	return (
		<>
			<DescriptionList isHorizontal isCompact termWidth="5ch">
				<BoxLine term="Stage">
					<Status status="done">
						{`Staged ${new Date(candidate.stage.staged_at).toLocaleDateString("en-US", { month: "short", day: "numeric" })}`}
					</Status>
				</BoxLine>
				{prod && (
					<BoxLine term="Prod">
						<Status status={prod.status}>
							<ExtLink href={konflux("releases", prod.name)} title={prod.name}>
								{prod.text}
							</ExtLink>
						</Status>
					</BoxLine>
				)}
			</DescriptionList>
			{warning && (
				<div style={line}>
					<Status status="warning">
						{warning === "no_pass" ? (
							"No passing CI run installed this candidate."
						) : (
							<>
								CI has not tested this candidate
								{warning.lastRun &&
									` since ${new Date(warning.lastRun).toLocaleDateString("en-US", { month: "short", day: "numeric" })}`}
								{warning.namesLatest && <>; CI now tests the {latest} build</>}.
							</>
						)}
					</Status>
				</div>
			)}
			<div style={line}>
				{ci.refresh === "rerun" && ci.passed ? (
					<Status status="warning">
						Re-run release-from-fbc with the{" "}
						<BuildLink build={ci.passed} konflux={konflux} /> build, which is
						newer and passed CI (optional).
						{candidate.prod && " ART may refuse it after a prod attempt."}{" "}
						<ClipboardCopy variant="inline-compact">
							{ci.passed.snapshot}
						</ClipboardCopy>
					</Status>
				) : ci.refresh === "waiting" ? (
					<Status status="running">
						Newer build {latest} is waiting on CI.
					</Status>
				) : (
					<Status status="done">Candidate is current.</Status>
				)}
			</div>
			{newest && (
				<NewestLine notIn={version} newest={newest} konflux={konflux} />
			)}
			<Table variant="compact" style={{ marginTop: "1rem" }}>
				<Thead>
					<Tr>
						<Th>Component</Th>
						<Th>Build</Th>
						{hasScans && <Th>CVEs</Th>}
					</Tr>
				</Thead>
				<Tbody>
					{candidate.components.map((c) => (
						<Tr key={c.name}>
							<Td>{componentLabel(c.name, app)}</Td>
							<Td>
								{c.nvr ? (
									<ExtLink href={c.build_url || null} title={c.nvr}>
										{nvrLabel(c.nvr)}
									</ExtLink>
								) : (
									noNvr
								)}
							</Td>
							{hasScans && (
								<Td>
									<ScanCell scan={c.scan} label={componentLabel(c.name, app)} />
								</Td>
							)}
						</Tr>
					))}
				</Tbody>
			</Table>
		</>
	);
}

const line = { marginTop: "0.5rem" };

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
	notIn,
	newest,
	konflux,
}: {
	/** The X.Y.Z of the candidate, which lacks the newest build's images. */
	notIn?: string;
	newest: NewestBuild;
	konflux: KonfluxLink;
}) {
	return (
		<div style={line}>
			Newest build <BuildLink build={newest} konflux={konflux} />
			{notIn && `: not in ${notIn}`}
			{newest.ci.state !== "unknown" && (
				<>
					{" · "}
					<CIStateText ci={newest.ci} konflux={konflux} />
				</>
			)}
		</div>
	);
}

function BoxLine({ term, children }: { term: string; children: ReactNode }) {
	return (
		<DescriptionListGroup>
			<DescriptionListTerm>{term}</DescriptionListTerm>
			<DescriptionListDescription>{children}</DescriptionListDescription>
		</DescriptionListGroup>
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
	none: (
		<Icon isInline>
			<span style={muted}>-</span>
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
			<FlexItem style={status === "none" ? muted : undefined}>
				{children}
			</FlexItem>
		</Flex>
	);
}

/** An image's fixable CVEs by severity, all its counts on click. */
function ScanCell({ scan, label }: { scan: ImageScan | null; label: string }) {
	if (scan?.state === "scanned" && scan.counts) {
		const fixable = fixableLabels(scan.counts);
		return (
			<Popover
				headerContent={label}
				bodyContent={<ScanDetail counts={scan.counts} url={scan.url} />}
				maxWidth="30rem"
			>
				<Button variant="plain" hasNoPadding>
					{fixable.length > 0 ? (
						<Flex component="span" gap={{ default: "gapXs" }}>
							{fixable.map((l) => (
								<Label key={l.text} isCompact color={l.color}>
									{l.text}
								</Label>
							))}
						</Flex>
					) : (
						<span style={muted}>0 fixable</span>
					)}
				</Button>
			</Popover>
		);
	}
	const cell = scanCell(scan);
	return (
		<ExtLink href={cell.url || null}>
			<span style={{ color: textColor[cell.color] }}>{cell.text}</span>
		</ExtLink>
	);
}

/** A scan's CVEs by severity, with and without a fix, and its Konflux page. */
function ScanDetail({ counts, url }: { counts: ScanCounts; url: string }) {
	const rows = [
		["Fixable", counts.fixable],
		["No fix", counts.no_fix],
	] as const;
	return (
		<>
			<Table
				variant="compact"
				borders={false}
				gridBreakPoint=""
				aria-label="CVEs by severity"
			>
				<Thead>
					<Tr>
						<Td />
						{SEVERITIES.map((s) => (
							<Th key={s} modifier="fitContent">
								{s.charAt(0).toUpperCase() + s.slice(1)}
							</Th>
						))}
					</Tr>
				</Thead>
				<Tbody>
					{rows.map(([row, n]) => (
						<Tr key={row}>
							<Th scope="row" modifier="fitContent">
								{row}
							</Th>
							{SEVERITIES.map((s) => (
								<Td key={s}>
									{n[s] > 0 ? (
										<Label isCompact color={SEVERITY_COLORS[s]}>
											{n[s]}
										</Label>
									) : (
										"—"
									)}
								</Td>
							))}
						</Tr>
					))}
				</Tbody>
			</Table>
			<div style={{ ...muted, marginTop: "0.5rem" }}>{BASES[counts.basis]}</div>
			{url && (
				<div style={{ marginTop: "0.5rem" }}>
					<ExtLink href={url}>Scan in Konflux</ExtLink>
				</div>
			)}
		</>
	);
}

// Prow job states as the OCP release controller words them; any other is grey.
const ciStates: Record<string, [string, LabelProps["status"]]> = {
	success: ["Succeeded", "success"],
	failure: ["Failed", "danger"],
	error: ["Error", "danger"],
};

const runState = (state: string) =>
	ciStates[state] ?? [
		state.charAt(0).toUpperCase() + state.slice(1),
		undefined,
	];

const nowrap = { whiteSpace: "nowrap" } as const;

/**
 * The periodic runs since the candidate's bundle was first built, or the
 * version's builds' without one, after the OCP release controller: a row per
 * build they installed, the candidate first.
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
	const hasCandidate = ci.builds.some((b) => b.kind === "candidate");
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>CI · periodic runs since {when(ci.since)}</CardTitle>
			<CardBody>
				<div style={{ ...muted, marginBottom: "0.5rem" }}>
					{hasCandidate && (
						<>
							<strong>Release candidate</strong>: the pinned build that ships as{" "}
							{version}. <strong>Staged</strong>: in the {version} stage
							release.{" "}
						</>
					)}
					<strong>In CI</strong>: reached the catalog CI installs from.
				</div>
				<Table variant="compact" aria-label="CI runs by installed build">
					<Thead>
						<Tr>
							<Th modifier="fitContent">Installed build</Th>
							<Th modifier="fitContent">State</Th>
							<Th>Jobs</Th>
						</Tr>
					</Thead>
					<Tbody>
						{ci.builds.map((b) => (
							<Tr key={`${b.kind} ${b.snapshot}`}>
								<Td style={nowrap}>
									{b.created_at ? (
										<BuildLink
											build={{ snapshot: b.snapshot, created_at: b.created_at }}
											konflux={konflux}
										/>
									) : b.kind === "unknown" ? (
										<span style={muted}>Unknown build</span>
									) : (
										"Quay not installed"
									)}
								</Td>
								<Td style={nowrap}>
									{b.kind === "candidate" ? (
										<Label isCompact color="blue">
											release candidate
										</Label>
									) : (
										b.ci && (
											<>
												<CIStateText ci={b.ci} konflux={konflux} />
												{hasCandidate &&
													`${b.ci.state !== "unknown" ? ", " : ""}not in ${version}`}
											</>
										)
									)}
								</Td>
								<Td>
									{b.jobs.length > 0 ? (
										<JobChips jobs={b.jobs} label={label} />
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

/** A chip per job, coloured by its newest run, opening it; every run on hover. */
function JobChips({
	jobs,
	label,
}: {
	jobs: CIJob[];
	label: (job: string) => string;
}) {
	return (
		<Flex gap={{ default: "gapXs" }}>
			{[...jobs]
				.sort((a, b) => label(a.job_name).localeCompare(label(b.job_name)))
				.map(({ job_name, runs }) => (
					<Tooltip
						key={job_name}
						content={lines(
							job_name,
							...runs.map(
								(r) => `${when(r.started_at)} · ${runState(r.state)[0]}`,
							),
						)}
					>
						<Label
							isCompact
							status={runState(runs[0].state)[1]}
							isClickable
							render={({ className, content, componentRef }) => (
								<a
									className={className}
									href={runs[0].prow_url}
									ref={componentRef}
									{...external}
								>
									{content}
								</a>
							)}
						>
							{label(job_name)}
							{runs.length > 1 && ` ×${runs.length}`}
						</Label>
					</Tooltip>
				))}
		</Flex>
	);
}

function TicketsCard({
	data,
	error,
	version,
	app,
	config,
}: {
	data?: BuildTickets;
	error?: Error;
	version: string;
	app: string;
	config?: DashboardConfig;
}) {
	const issues = data?.tickets;
	const hasIssues = (issues ?? []).length > 0;
	const jira = config?.jira_project
		? jiraSearchUrl(
				ticketsJql(config.jira_project, version, issues ?? []),
				config.jira_base_url,
			)
		: null;
	const zTip = issues?.some((t) => t.fix_version !== version)
		? data?.z_since
			? `.z tickets are listed when a candidate commit newer than ${data.z_since.replace(/^[a-z]+-v/, "")}'s candidate names them.`
			: ".z tickets are listed when a candidate commit on the release branch names them."
		: undefined;

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={jira} link="Jira">
				Tickets
				{hasIssues && ` (${issues!.length})`}
			</LinkedCardTitle>
			<CardBody>
				{data && data.not_compared.length > 0 && (
					<div style={{ ...muted, marginBottom: "0.5rem" }}>
						Not compared:{" "}
						{data.not_compared
							.map((c) => `${componentLabel(c.component, app)} (${c.reason})`)
							.join(", ")}
					</div>
				)}
				{hasIssues ? (
					<IssuesTable
						issues={issues!}
						hasBuild={!!data?.build}
						zTip={zTip}
						app={app}
					/>
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

/** A ticket's status as its icon, named on hover. */
function TicketStatus({ status }: { status: string }) {
	const { icon: StatusIcon, verified } = statusIcon(status);
	return (
		<Tooltip content={status} aria="none">
			<Icon
				status={verified ? "success" : undefined}
				isInline
				role="img"
				aria-label={status}
			>
				<StatusIcon />
			</Icon>
		</Tooltip>
	);
}

const commitLabel = (c: BuildCommit, app: string) =>
	`${componentLabel(c.component, app)}@${c.commit_sha.slice(0, 7)}`;

// Tickets not in the build sort first, then "-", then by commit.
const inBuildKey = (t: BuildTicket, app: string) =>
	t.in_build.length > 0
		? `2 ${t.in_build.map((c) => commitLabel(c, app)).join(" ")}`
		: t.not_in_build.length > 0
			? "0"
			: "1";

const inBuildTip = lines(
	"Commits: the candidate's release-branch commits that name the ticket.",
	"Not in build: a default-branch commit names it and no candidate commit does.",
	"-: no compared commit names it, which proves nothing.",
);

// The ticket table's columns, in order.
const ISSUES_COLUMNS = [
	"key",
	"type",
	"summary",
	"status",
	"target",
	"inBuild",
];

function IssuesTable({
	issues,
	hasBuild,
	zTip,
	app,
}: {
	issues: BuildTicket[];
	hasBuild: boolean;
	/** How the .z tickets were found, when any are listed. */
	zTip?: string;
	app: string;
}) {
	const [activeSortKey, setActiveSortKey] = useState<string | undefined>(
		undefined,
	);
	const [activeSortDirection, setActiveSortDirection] = useState<
		"asc" | "desc" | undefined
	>(undefined);

	const sortedIssues = useMemo(() => {
		if (activeSortKey === undefined || activeSortDirection === undefined) {
			return issues;
		}
		return [...issues].sort((a, b) => {
			let cmp = 0;
			switch (activeSortKey) {
				case "type":
					cmp = a.issue_type.localeCompare(b.issue_type);
					break;
				case "status":
					cmp = a.status.localeCompare(b.status);
					break;
				case "inBuild":
					cmp = inBuildKey(a, app).localeCompare(inBuildKey(b, app));
					break;
			}
			return activeSortDirection === "asc" ? cmp : -cmp;
		});
	}, [issues, activeSortKey, activeSortDirection, app]);

	const getSortParams = (columnKey: string): ThProps["sort"] => ({
		sortBy: {
			index: activeSortKey ? ISSUES_COLUMNS.indexOf(activeSortKey) : undefined,
			direction: activeSortDirection,
		},
		onSort: (_event, _index, direction) => {
			setActiveSortKey(columnKey);
			setActiveSortDirection(direction);
		},
		columnIndex: ISSUES_COLUMNS.indexOf(columnKey),
	});

	return (
		<Table variant="compact" style={{ tableLayout: "auto" }}>
			<Thead>
				<Tr>
					<Th style={{ whiteSpace: "nowrap" }}>Key</Th>
					<Th sort={getSortParams("type")} modifier="fitContent">
						Type
					</Th>
					<Th>Summary</Th>
					<Th sort={getSortParams("status")} modifier="fitContent">
						Status
					</Th>
					<Th info={zTip ? { tooltip: zTip } : undefined} modifier="fitContent">
						Target
					</Th>
					{hasBuild && (
						<Th
							sort={getSortParams("inBuild")}
							info={{ tooltip: inBuildTip }}
							modifier="fitContent"
						>
							In build
						</Th>
					)}
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
						<Td>
							<TicketStatus status={issue.status} />
						</Td>
						<Td>{issue.fix_version.replace(/^[a-z]+-v/, "")}</Td>
						{hasBuild && (
							<Td style={{ whiteSpace: "nowrap" }}>
								{issue.in_build.length > 0 ? (
									issue.in_build.map((c) => (
										<div key={`${c.component}@${c.commit_sha}`}>
											<a href={c.commit_url} {...external} title={c.component}>
												{commitLabel(c, app)}
											</a>
										</div>
									))
								) : issue.not_in_build.length > 0 ? (
									<>
										<Label isCompact color="red">
											Not in build
										</Label>
										{missingCommits(issue.not_in_build, app).map((c) => (
											<a
												key={c.sha}
												href={c.url}
												{...external}
												title={c.title}
												style={{ marginLeft: "0.5rem" }}
											>
												{c.sha.slice(0, 7)}
											</a>
										))}
									</>
								) : (
									"-"
								)}
							</Td>
						)}
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}

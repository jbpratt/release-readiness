import {
	Breadcrumb,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
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
	Panel,
	PanelMain,
	PanelMainBody,
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
	InfoCircleIcon,
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
	BuildRow,
	BuildTicket,
	BuildTickets,
	CandidateBuild,
	CIJob,
	DashboardConfig,
	ImageScan,
	ReleaseCandidate,
	ScanCounts,
} from "../api/types";
import StatusLabel from "../components/StatusLabel";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { due, relative } from "../utils/format";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	ticketsJql,
} from "../utils/links";
import {
	BASES,
	buildTitle,
	componentLabel,
	fixableLabels,
	jobLabels,
	missingCommits,
	notVerified,
	nvrLabel,
	releaseLine,
	SEVERITIES,
	SEVERITY_COLORS,
	type StatusLine,
	scanCell,
	stageLines,
} from "../utils/releaseDetail";

/** A Konflux UI page of the version's application. */
type KonfluxLink = (
	kind?: "snapshots" | "releases",
	name?: string,
) => string | null;

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };
const textColor = {
	red: "var(--pf-t--global--text--color--status--danger--default)",
	orange: "var(--pf-t--global--text--color--status--warning--default)",
	grey: muted.color,
};

export default function ReleaseDetail() {
	const { version } = useParams<{ version: string }>();
	const config = useConfig();

	const { data: release, loading: loadingRelease } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);
	const { data: tickets, error: ticketsError } = useCachedFetch(
		version ? `buildTickets:${version}` : null,
		() => getBuildTickets(version!),
	);
	const candidate = useCachedFetch(
		version ? `candidate:${version}` : null,
		() => getCandidate(version!),
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
	const ticket = release.release_ticket_key;
	const app = release.konflux_application ?? "";
	const target = release.due_date ?? release.release_date;
	const dueDate = target
		? due(target, candidate.data?.shipped ?? release.released)
		: undefined;
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
			</Breadcrumb>
			<Flex
				alignItems={{ default: "alignItemsBaseline" }}
				style={{ marginBottom: "1rem" }}
			>
				<Title headingLevel="h1">{displayName}</Title>
				<span style={dueDate?.color && { color: textColor[dueDate.color] }}>
					{dueDate ? `Due ${dueDate.text}` : "No due date"}
				</span>
				{ticket && (
					<a
						href={jiraIssueUrl(
							ticket,
							config?.jira_base_url || "https://redhat.atlassian.net",
						)}
						{...external}
					>
						{ticket}
					</a>
				)}
				<span>{release.release_ticket_assignee || "Unassigned"}</span>
			</Flex>

			<BuildsCard
				data={candidate.data}
				error={candidate.error}
				app={app}
				konflux={konflux}
			/>

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

/** The builds that matter to the version, one box each, and the candidate's images. */
function BuildsCard({
	data,
	error,
	app,
	konflux,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	app: string;
	konflux: KonfluxLink;
}) {
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={konflux()} link="Konflux">
				Builds
			</LinkedCardTitle>
			<CardBody>
				{error ? (
					error.message
				) : !data ? (
					<Spinner size="md" />
				) : data.shipped ? (
					"Shipped."
				) : !data.candidate ? (
					data.reason.charAt(0).toUpperCase() + data.reason.slice(1)
				) : (
					<Builds
						builds={data.builds}
						candidate={data.candidate}
						app={app}
						konflux={konflux}
					/>
				)}
			</CardBody>
		</Card>
	);
}

function Builds({
	builds,
	candidate,
	app,
	konflux,
}: {
	builds: BuildRow[];
	candidate: CandidateBuild;
	app: string;
	konflux: KonfluxLink;
}) {
	const hasScans = candidate.components.some((c) => c.scan);
	const oldestFirst = [...builds].sort(
		(a, b) => Date.parse(a.created_at) - Date.parse(b.created_at),
	);
	return (
		<>
			<Flex alignItems={{ default: "alignItemsStretch" }}>
				{oldestFirst.map((b) => (
					<FlexItem
						key={b.snapshot}
						flex={{ default: "flex_1" }}
						style={{ minWidth: "18rem" }}
					>
						<BuildBox
							build={b}
							source={candidate.source}
							app={app}
							konflux={konflux}
						/>
					</FlexItem>
				))}
			</Flex>
			<Title headingLevel="h3" size="md" style={{ marginTop: "1rem" }}>
				Images in the {candidate.source === "staged" ? "staged" : "latest"}{" "}
				build
			</Title>
			<Table variant="compact">
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

/** A build by its role: when it was built, and whether it is in stage, in prod and tested. */
function BuildBox({
	build,
	source,
	app,
	konflux,
}: {
	build: BuildRow;
	source: CandidateBuild["source"];
	app: string;
	konflux: KonfluxLink;
}) {
	return (
		<Panel variant="bordered" style={{ height: "100%" }}>
			<PanelMain>
				<PanelMainBody>
					<Title headingLevel="h3" size="md">
						{buildTitle(build.roles, source)}
					</Title>
					<ExtLink
						href={konflux("snapshots", build.snapshot)}
						title={build.snapshot}
					>
						{new Date(build.created_at).toLocaleString(undefined, {
							month: "short",
							day: "numeric",
							hour: "2-digit",
							minute: "2-digit",
							hourCycle: "h23",
						})}
					</ExtLink>
					<DescriptionList
						isHorizontal
						isCompact
						termWidth="5ch"
						style={{ marginTop: "0.75rem" }}
					>
						<BoxLine term="Stage">
							<StatusLines
								lines={stageLines(build.stage, app)}
								konflux={konflux}
							/>
						</BoxLine>
						{build.prod && (
							<BoxLine term="Prod">
								<StatusLines
									lines={[releaseLine(build.prod)]}
									konflux={konflux}
								/>
							</BoxLine>
						)}
						<BoxLine term="CI">
							<CICell jobs={build.ci} />
						</BoxLine>
					</DescriptionList>
				</PanelMainBody>
			</PanelMain>
		</Panel>
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
	waiting: (
		<Icon status="warning" isInline>
			<ExclamationTriangleIcon />
		</Icon>
	),
	running: (
		<Icon status="info" isInline>
			<InProgressIcon />
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

/** Status lines, a line about a Release linked to it, a line's why on an info icon. */
function StatusLines({
	lines,
	konflux,
}: {
	lines: StatusLine[];
	konflux: KonfluxLink;
}) {
	return lines.map((l) => (
		<Status key={l.text} status={l.status}>
			{l.release ? (
				<ExtLink href={konflux("releases", l.release)} title={l.release}>
					{l.text}
				</ExtLink>
			) : (
				l.text
			)}
			{l.info && (
				<Tooltip content={l.info}>
					<Button
						variant="plain"
						hasNoPadding
						aria-label={l.info}
						icon={<InfoCircleIcon />}
						style={{ marginLeft: "0.25rem" }}
					/>
				</Tooltip>
			)}
		</Status>
	));
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

/** A label per job, opening its newest run of the build, the run on hover. */
function CICell({ jobs }: { jobs: CIJob[] }) {
	if (jobs.length === 0) {
		return <Status status="none">Not tested</Status>;
	}
	const labels = jobLabels(jobs.map((j) => j.job_name));
	return (
		<Flex gap={{ default: "gapXs" }}>
			{jobs.map((j, i) => {
				const [state, status] = ciStates[j.state] ?? [
					j.state.charAt(0).toUpperCase() + j.state.slice(1),
					undefined,
				];
				return (
					<Tooltip
						key={j.job_name}
						content={lines(
							j.job_name,
							state,
							j.started_at ? relative(j.started_at) : undefined,
							j.runs > 1 ? `${j.runs} runs of this build` : undefined,
						)}
					>
						<Label
							isCompact
							status={status}
							isClickable
							render={({ className, content, componentRef }) => (
								<a
									className={className}
									href={j.prow_url}
									ref={componentRef}
									{...external}
								>
									{content}
								</a>
							)}
						>
							{labels[i]}
						</Label>
					</Tooltip>
				);
			})}
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
	const open = notVerified(issues ?? []);
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
				{open > 0 && ` · ${open} not verified`}
			</LinkedCardTitle>
			<CardBody>
				{data && !data.build && data.reason !== "shipped" && (
					<div style={{ ...muted, marginBottom: "0.5rem" }}>
						Not checked against a build: {data.reason}
					</div>
				)}
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
					<Th sort={getSortParams("type")} style={{ whiteSpace: "nowrap" }}>
						Type
					</Th>
					<Th>Summary</Th>
					<Th
						sort={getSortParams("status")}
						style={{ whiteSpace: "nowrap", minWidth: "110px" }}
					>
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
							<StatusLabel status={issue.status} />
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

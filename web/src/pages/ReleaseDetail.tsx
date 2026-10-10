import {
	Breadcrumb,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
	EmptyState,
	EmptyStateBody,
	Flex,
	FlexItem,
	Label,
	type LabelProps,
	PageSection,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import { ExternalLinkAltIcon } from "@patternfly/react-icons";
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
	KonfluxRelease,
	ReleaseCandidate,
	StageFlag,
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
	componentLabel,
	jobLabels,
	missingCommits,
	notVerified,
	nvrLabel,
	scanCell,
} from "../utils/releaseDetail";
import { releaseStatus } from "../utils/releaseStatus";

/** A Konflux UI page of the version's application. */
type KonfluxLink = (
	kind?: "snapshots" | "releases",
	name?: string,
) => string | null;

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };
const textColor = {
	green: "var(--pf-t--global--text--color--status--success--default)",
	red: "var(--pf-t--global--text--color--status--danger--default)",
	orange: "var(--pf-t--global--text--color--status--warning--default)",
	blue: "var(--pf-t--global--text--color--status--info--default)",
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

/** The builds that matter to the version, one row each, and the candidate's images. */
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
					data.reason
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
	const hasProd = builds.some((b) => b.prod);
	const hasScans = candidate.components.some((c) => c.scan);
	return (
		<>
			<Table variant="compact">
				<Thead>
					<Tr>
						<Th>Build</Th>
						<Th>Stage</Th>
						{hasProd && <Th>Prod</Th>}
						<Th>CI</Th>
					</Tr>
				</Thead>
				<Tbody>
					{builds.map((b) => (
						<Tr key={b.snapshot}>
							<Td style={{ whiteSpace: "nowrap" }}>
								<ExtLink
									href={konflux("snapshots", b.snapshot)}
									title={b.snapshot}
								>
									{new Date(b.created_at).toLocaleString(undefined, {
										month: "short",
										day: "numeric",
										hour: "2-digit",
										minute: "2-digit",
										hourCycle: "h23",
									})}
								</ExtLink>
								{b.roles.map((role) => (
									<RoleLabel
										key={role}
										role={role}
										source={candidate.source}
										app={app}
									/>
								))}
							</Td>
							<Td>
								<StageCell stage={b.stage} app={app} konflux={konflux} />
							</Td>
							{hasProd && (
								<Td>
									{b.prod && (
										<Tooltip
											content={lines(b.prod.name, releaseDetail(b.prod))}
										>
											<span>
												<ReleaseLink release={b.prod} konflux={konflux} />
											</span>
										</Tooltip>
									)}
								</Td>
							)}
							<Td>
								<CICell jobs={b.ci} />
							</Td>
						</Tr>
					))}
				</Tbody>
			</Table>
			<Title headingLevel="h3" size="md" style={{ marginTop: "1rem" }}>
				Images in the candidate
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
									<ScanCell scan={c.scan} />
								</Td>
							)}
						</Tr>
					))}
				</Tbody>
			</Table>
		</>
	);
}

/** A build's role, what it means on hover. */
function RoleLabel({
	role,
	source,
	app,
}: {
	role: BuildRow["roles"][number];
	source: CandidateBuild["source"];
	app: string;
}) {
	const tip =
		role === "newest"
			? `The newest build of ${app}.`
			: role === "last_tested"
				? "The newest build a periodic CI run tested."
				: source === "staged"
					? "This version's build in stage. Its images are listed below."
					: "No build of this version is in stage yet, so the stream's newest build. Its images are listed below.";
	return (
		<Tooltip content={tip}>
			<Label
				isCompact
				color={role === "candidate" ? "blue" : "grey"}
				style={{ marginLeft: "0.5rem" }}
			>
				{role === "last_tested" ? "last tested" : role}
			</Label>
		</Tooltip>
	);
}

/** A Release's status in its colour, linked to its Konflux page. */
function ReleaseLink({
	release,
	konflux,
}: {
	release: KonfluxRelease;
	konflux: KonfluxLink;
}) {
	const status = releaseStatus(release);
	return (
		<ExtLink href={konflux("releases", release.name)}>
			<span style={{ color: textColor[status.color] }}>{status.text}</span>
		</ExtLink>
	);
}

const releaseDetail = (r: KonfluxRelease) => {
	const status = releaseStatus(r);
	return "detail" in status ? status.detail : undefined;
};

/** Whether a build reached stage, with what keeps it out on hover. */
function StageCell({
	stage,
	app,
	konflux,
}: {
	stage: StageFlag;
	app: string;
	konflux: KonfluxLink;
}) {
	if (stage.state === "staged") {
		const at = stage.staged_at ? new Date(stage.staged_at) : null;
		return (
			<span style={{ color: textColor.green }} title={at?.toLocaleString()}>
				Staged
				{at &&
					` ${at.toLocaleDateString(undefined, { month: "short", day: "numeric" })}`}
			</span>
		);
	}
	const r = stage.release;
	return (
		<Tooltip
			content={
				stage.state === "unknown"
					? `No stage Release for ${app} yet.`
					: lines(
							`${stage.not_staged.length} of ${stage.total} images not in stage: ${stage.not_staged.map((c) => componentLabel(c, app)).join(", ")}`,
							r?.name,
							r ? releaseDetail(r) : undefined,
						)
			}
		>
			<span>
				{stage.state === "unknown" ? (
					<span style={muted}>-</span>
				) : r ? (
					<ReleaseLink release={r} konflux={konflux} />
				) : (
					<span style={{ color: textColor.orange }}>Not in stage</span>
				)}
			</span>
		</Tooltip>
	);
}

/** An image's CVE scan linked to its build PipelineRun, the counts on hover. */
function ScanCell({ scan }: { scan: ImageScan | null }) {
	const cell = scanCell(scan);
	const link = (
		<ExtLink href={cell.url || null}>
			<span style={cell.color && { color: textColor[cell.color] }}>
				{cell.text}
			</span>
		</ExtLink>
	);
	return cell.detail ? (
		<Tooltip
			maxWidth="30rem"
			content={cell.detail.map((line) => <div key={line}>{line}</div>)}
		>
			<span>{link}</span>
		</Tooltip>
	) : (
		link
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
		return <span style={muted}>Not tested</span>;
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

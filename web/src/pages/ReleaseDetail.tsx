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
	Icon,
	PageSection,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	ExclamationTriangleIcon,
	ExternalLinkAltIcon,
	InfoCircleIcon,
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
import { Fragment, type ReactNode, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getBuildTickets, getCandidate, getRelease } from "../api/client";
import type {
	BuildCommit,
	BuildTicket,
	BuildTickets,
	CandidateBuild,
	CandidateCI,
	CIJob,
	DashboardConfig,
	KonfluxRelease,
	ReleaseCandidate,
} from "../api/types";
import StatusLabel from "../components/StatusLabel";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { dueText, relative } from "../utils/format";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
} from "../utils/links";
import { jobShortName, notVerified } from "../utils/releaseDetail";
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
			<Flex
				alignItems={{ default: "alignItemsBaseline" }}
				style={{ marginBottom: "1rem" }}
			>
				<Title headingLevel="h1">{displayName}</Title>
				<span>{dueText(release.due_date ?? release.release_date)}</span>
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

			<CandidateCard
				data={candidate.data}
				error={candidate.error}
				app={app}
				konflux={konflux}
			/>

			{candidate.data?.candidate && <CICard ci={candidate.data.ci} app={app} />}

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
	children,
}: {
	href: string | null;
	children: ReactNode;
}) {
	return href ? (
		<a href={href} {...external}>
			{children}
		</a>
	) : (
		children
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

/** A Release linked to its Konflux page, then its status, with the detail on hover. */
function ReleaseStatus({
	release,
	href,
}: {
	release: KonfluxRelease;
	href: string | null;
}) {
	const status = releaseStatus(release);
	return (
		<>
			<ExtLink href={href}>{release.name}</ExtLink>:{" "}
			<span style={{ color: textColor[status.color] }}>{status.text}</span>
			{"detail" in status && status.detail && (
				<>
					{" "}
					<Tooltip content={status.detail}>
						<InfoCircleIcon style={muted} />
					</Tooltip>
				</>
			)}
		</>
	);
}

const noNvr = (
	<span title="Not in ART build history yet" style={muted}>
		-
	</span>
);

/** The build up for release, whether it reached stage, and its images. */
function CandidateCard({
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
				Candidate build
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
					<Candidate
						data={data}
						build={data.candidate}
						app={app}
						konflux={konflux}
					/>
				)}
			</CardBody>
		</Card>
	);
}

function Candidate({
	data: { prod, stage },
	build,
	app,
	konflux,
}: {
	data: ReleaseCandidate;
	build: CandidateBuild;
	app: string;
	konflux: KonfluxLink;
}) {
	return (
		<>
			<div>
				<ExtLink href={konflux("snapshots", build.snapshot)}>
					{build.snapshot}
				</ExtLink>{" "}
				· built {relative(build.created_at)} ·{" "}
				{build.source === "staged"
					? `in stage since ${relative(build.staged_at!)}`
					: `newest build of ${app}`}
			</div>
			{prod && (
				<div>
					Prod:{" "}
					<ReleaseStatus release={prod} href={konflux("releases", prod.name)} />
				</div>
			)}
			{stage?.state === "not_staged" && (
				<div>
					<Icon status="warning" isInline>
						<ExclamationTriangleIcon />
					</Icon>{" "}
					{stage.snapshot !== build.snapshot && (
						<>
							Newest build{" "}
							<ExtLink href={konflux("snapshots", stage.snapshot)}>
								{stage.snapshot}
							</ExtLink>{" "}
							({relative(stage.created_at)}):{" "}
						</>
					)}
					<Tooltip
						content={stage.not_staged
							.map((c) => componentLabel(c, app))
							.join(", ")}
					>
						<span style={{ textDecoration: "underline dotted" }}>
							{stage.not_staged.length} of {stage.total} images not in stage
						</span>
					</Tooltip>
					{stage.release && (
						<>
							.{" "}
							<ReleaseStatus
								release={stage.release}
								href={konflux("releases", stage.release.name)}
							/>
						</>
					)}
				</div>
			)}
			{stage?.state === "unknown" && (
				<div style={muted}>No stage Release for {app} yet.</div>
			)}
			<Table variant="compact" style={{ marginTop: "0.5rem" }}>
				<Thead>
					<Tr>
						<Th>Component</Th>
						<Th>Build</Th>
					</Tr>
				</Thead>
				<Tbody>
					{build.components.map((c) => (
						<Tr key={c.name}>
							<Td>{componentLabel(c.name, app)}</Td>
							<Td>
								{c.nvr ? (
									<ExtLink href={c.build_url || null}>{c.nvr}</ExtLink>
								) : (
									noNvr
								)}
							</Td>
						</Tr>
					))}
				</Tbody>
			</Table>
		</>
	);
}

// Prow job states as the OCP release controller words them; any other is grey.
const ciStates: Record<string, [string, keyof typeof textColor]> = {
	success: ["Succeeded", "green"],
	failure: ["Failed", "red"],
	error: ["Error", "red"],
};

/** The periodic runs that tested the candidate, else the build they last tested. */
function CICard({ ci, app }: { ci: CandidateCI; app: string }) {
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>CI</CardTitle>
			<CardBody>
				{ci.jobs.length > 0 ? (
					<Jobs jobs={ci.jobs} />
				) : ci.last_tested ? (
					<>
						<div>Not tested yet.</div>
						<div style={muted}>
							Last tested: {ci.last_tested.bundle_nvr || noNvr}
							{ci.last_tested.tested_at &&
								` (${relative(ci.last_tested.tested_at)})`}
						</div>
						<Jobs jobs={ci.last_tested.jobs} />
					</>
				) : (
					`No periodic run has tested a build of ${app} yet.`
				)}
			</CardBody>
		</Card>
	);
}

/** One line per job: its short name linked to the run, its state, its full name. */
function Jobs({ jobs }: { jobs: CIJob[] }) {
	return (
		<div
			style={{
				display: "grid",
				gridTemplateColumns: "auto auto 1fr",
				columnGap: "1rem",
			}}
		>
			{jobs.map((j) => {
				const [state, color] = ciStates[j.state] ?? [
					j.state.charAt(0).toUpperCase() + j.state.slice(1),
					"grey",
				];
				return (
					<Fragment key={j.job_name}>
						<a href={j.prow_url} {...external}>
							{jobShortName(j.job_name)}
						</a>
						<span>
							<span style={{ color: textColor[color] }}>{state}</span>
							{j.runs > 1 && ` (${j.runs} runs)`}
						</span>
						<span style={muted}>{j.job_name}</span>
					</Fragment>
				);
			})}
		</div>
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
				`project=${config.jira_project} AND "Target Version"="${version}"`,
				config.jira_base_url,
			)
		: null;

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={jira} link="Jira">
				Tickets for {formatReleaseName(version)}
				{hasIssues && ` (${issues!.length})`}
				{open > 0 && ` · ${open} not verified`}
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
					<IssuesTable issues={issues!} hasBuild={!!data?.build} app={app} />
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

// The page already names the release, so quay-3-18-quay-clair reads clair.
const componentLabel = (component: string, app?: string) => {
	const prefix = `${app}-quay-`;
	return app && component.startsWith(prefix)
		? component.slice(prefix.length)
		: component;
};

const commitLabel = (c: BuildCommit, app?: string) =>
	`${componentLabel(c.component, app)}@${c.commit_sha.slice(0, 7)}`;

// The ticket table's columns, in order.
const ISSUES_COLUMNS = ["key", "summary", "status", "target", "inBuild"];

function IssuesTable({
	issues,
	hasBuild,
	app,
}: {
	issues: BuildTicket[];
	hasBuild: boolean;
	app?: string;
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
				case "status":
					cmp = a.status.localeCompare(b.status);
					break;
				case "inBuild":
					// "" for no commit, so "-" sorts first.
					cmp = a.in_build
						.map((c) => commitLabel(c, app))
						.join(" ")
						.localeCompare(
							b.in_build.map((c) => commitLabel(c, app)).join(" "),
						);
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
					<Th>Summary</Th>
					<Th
						sort={getSortParams("status")}
						style={{ whiteSpace: "nowrap", minWidth: "110px" }}
					>
						Status
					</Th>
					<Th>Target</Th>
					<Th sort={getSortParams("inBuild")} modifier="fitContent">
						In build
					</Th>
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
						<Td style={{ whiteSpace: "normal", wordBreak: "break-word" }}>
							{issue.summary}
						</Td>
						<Td>
							<StatusLabel status={issue.status} />
						</Td>
						<Td>{issue.fix_version.replace(/^[a-z]+-v/, "")}</Td>
						<Td style={{ whiteSpace: "nowrap" }}>
							{hasBuild &&
								(issue.in_build.length === 0
									? "-"
									: issue.in_build.map((c) => (
											<div key={`${c.component}@${c.commit_sha}`}>
												<a
													href={c.commit_url}
													{...external}
													title={c.component}
												>
													{commitLabel(c, app)}
												</a>
											</div>
										)))}
						</Td>
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}

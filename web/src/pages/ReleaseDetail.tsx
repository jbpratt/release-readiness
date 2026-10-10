import { ColumnManagementModal } from "@patternfly/react-component-groups";
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
	HelperText,
	HelperTextItem,
	MenuToggle,
	PageSection,
	Popover,
	Select,
	SelectList,
	SelectOption,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	ArrowRightIcon,
	ColumnsIcon,
	OutlinedQuestionCircleIcon,
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
import { useMemo, useState } from "react";
import { Link, Navigate, useParams, useSearchParams } from "react-router-dom";
import {
	getBuildTickets,
	getRelease,
	getReleaseReadiness,
	getStaged,
} from "../api/client";
import type {
	BuildCommit,
	BuildTicket,
	BuildTickets,
	DashboardConfig,
	ReleaseVersion,
} from "../api/types";
import PriorityLabel from "../components/PriorityLabel";
import ReadinessPipeline from "../components/ReadinessPipeline";
import StatusLabel from "../components/StatusLabel";
import { useCachedFetch } from "../hooks/useCachedFetch";
import {
	type ColumnDef,
	useColumnManagement,
} from "../hooks/useColumnManagement";
import { useConfig } from "../hooks/useConfig";
import { relative } from "../utils/format";
import { formatReleaseName, jiraIssueUrl, minorVersion } from "../utils/links";
import { dueText, readiness } from "../utils/readiness";

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
	const [searchParams] = useSearchParams();

	// Old deep links carry snapshot history filters; send them to the history page.
	if (searchParams.has("app") || searchParams.has("with_release")) {
		return (
			<Navigate
				to={{ pathname: "snapshots", search: `?${searchParams}` }}
				replace
			/>
		);
	}

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
	const minor = minorVersion(release.name);

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
						target="_blank"
						rel="noopener noreferrer"
					>
						{ticket}
					</a>
				)}
				<span>{release.release_ticket_assignee || "Unassigned"}</span>
				<FlexItem align={{ default: "alignRight" }}>
					<Tooltip
						content={`The stream: every ${minor}.z build, the same for each ${minor} version.`}
					>
						<Button
							variant="secondary"
							icon={<ArrowRightIcon />}
							iconPosition="end"
							component={(props: object) => (
								<Link
									{...props}
									to={`/releases/${encodeURIComponent(release.name)}/snapshots`}
								/>
							)}
						>
							Builds, snapshots and CI of{" "}
							{release.konflux_application || "this release"}
						</Button>
					</Tooltip>
				</FlexItem>
			</Flex>

			<ReadinessCard
				release={release}
				tickets={tickets?.tickets}
				ticketsError={ticketsError}
				jiraEnabled={config?.jira_enabled !== false}
			/>

			<IssuesCard
				data={tickets}
				error={ticketsError}
				version={version!}
				app={release.konflux_application}
				config={config}
			/>
		</PageSection>
	);
}

/** The readiness pipeline: what ART staged, the tickets, and whether the version shipped. */
function ReadinessCard({
	release,
	tickets,
	ticketsError,
	jiraEnabled,
}: {
	release: ReleaseVersion;
	tickets?: BuildTicket[];
	ticketsError?: Error;
	jiraEnabled: boolean;
}) {
	const { name } = release;
	const staged = useCachedFetch(`staged:${name}`, () => getStaged(name));
	const shipped = useCachedFetch(`readiness:${name}`, () =>
		getReleaseReadiness(name),
	);
	const error = staged.error ?? shipped.error ?? ticketsError;
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Readiness</CardTitle>
			<CardBody>
				{error ? (
					error.message
				) : staged.data && shipped.data && tickets ? (
					<ReadinessPipeline
						r={readiness({
							release,
							staged: staged.data,
							tickets,
							shipped: shipped.data.shipped,
							jiraEnabled,
							now: Date.now(),
						})}
					/>
				) : (
					<Spinner size="md" />
				)}
			</CardBody>
		</Card>
	);
}

const ISSUES_COLUMNS: ColumnDef[] = [
	{ key: "key", label: "Key" },
	{ key: "type", label: "Type" },
	{ key: "summary", label: "Summary" },
	{ key: "priority", label: "Priority" },
	{ key: "status", label: "Status" },
	{ key: "assignee", label: "Assignee" },
	{ key: "qaContact", label: "QA Contact" },
	{ key: "target", label: "Target" },
	{ key: "inBuild", label: "In build" },
];

const priorityWeight: Record<string, number> = {
	blocker: 0,
	critical: 1,
	major: 2,
	normal: 3,
	minor: 4,
};

function buildJQL(
	config: DashboardConfig | undefined,
	version: string,
): string | undefined {
	if (!config?.jira_project) return undefined;
	const project = config.jira_project;
	return `project=${project} AND "Target Version"="${version}"`;
}

function IssuesCard({
	data,
	error,
	version,
	app,
	config,
}: {
	data?: BuildTickets;
	error?: Error;
	version: string;
	app?: string;
	config?: DashboardConfig;
}) {
	const issues = data?.tickets;
	const [typeFilter, setTypeFilter] = useState<string>("All");
	const [typeSelectOpen, setTypeSelectOpen] = useState(false);
	const columnMgmt = useColumnManagement("rr-columns-issues", ISSUES_COLUMNS);

	const issueTypes = useMemo(() => {
		const types = new Set((issues ?? []).map((i) => i.issue_type));
		return ["All", ...Array.from(types).sort()];
	}, [issues]);

	const filteredIssues = useMemo(
		() =>
			typeFilter === "All"
				? (issues ?? [])
				: (issues ?? []).filter((i) => i.issue_type === typeFilter),
		[issues, typeFilter],
	);
	const hasIssues = (issues ?? []).length > 0;

	const jql = buildJQL(config, version);

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<FlexItem>
						Tickets for {formatReleaseName(version)}
						{hasIssues && ` (${filteredIssues.length})`}
						{jql && (
							<Popover headerContent="JQL Query" bodyContent={jql}>
								<Button
									variant="plain"
									aria-label="Show JQL query"
									style={{ padding: "0 0 0 0.25rem" }}
								>
									<OutlinedQuestionCircleIcon />
								</Button>
							</Popover>
						)}
					</FlexItem>
					{hasIssues && (
						<FlexItem>
							<Flex
								alignItems={{ default: "alignItemsCenter" }}
								spaceItems={{ default: "spaceItemsMd" }}
							>
								<FlexItem>
									<Button
										variant="plain"
										aria-label="Manage columns"
										onClick={columnMgmt.openModal}
									>
										<ColumnsIcon />
									</Button>
								</FlexItem>
								<FlexItem>
									<Select
										isOpen={typeSelectOpen}
										selected={typeFilter}
										onSelect={(_e, value) => {
											setTypeFilter(value as string);
											setTypeSelectOpen(false);
										}}
										onOpenChange={setTypeSelectOpen}
										toggle={(toggleRef) => (
											<MenuToggle
												ref={toggleRef}
												onClick={() => setTypeSelectOpen((prev) => !prev)}
												isExpanded={typeSelectOpen}
											>
												Type: {typeFilter}
											</MenuToggle>
										)}
									>
										<SelectList>
											{issueTypes.map((t) => (
												<SelectOption key={t} value={t}>
													{t}
												</SelectOption>
											))}
										</SelectList>
									</Select>
								</FlexItem>
							</Flex>
						</FlexItem>
					)}
				</Flex>
			</CardTitle>
			<CardBody>
				{data && (
					<div style={{ marginBottom: "0.5rem" }}>
						<div>
							{data.build ? (
								<>
									Build <code>{data.build.snapshot}</code>, STAGE{" "}
									{relative(data.build.completed_at)}
								</>
							) : (
								`No STAGE build: ${data.reason}`
							)}
						</div>
						{data.not_compared.length > 0 && (
							<div>
								Not compared:{" "}
								{data.not_compared
									.map(
										(c) => `${componentLabel(c.component, app)} (${c.reason})`,
									)
									.join(", ")}
							</div>
						)}
					</div>
				)}
				<HelperText style={{ marginBottom: "0.5rem" }}>
					<HelperTextItem>
						Target Version tickets from Jira, plus .z tickets a build commit
						names. In build links the commits that name the ticket.
					</HelperTextItem>
				</HelperText>
				{hasIssues ? (
					<IssuesTable
						issues={filteredIssues}
						hasBuild={!!data?.build}
						app={app}
						columnMgmt={columnMgmt}
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

// The page already names the release, so quay-3-18-quay-clair reads clair.
const componentLabel = (component: string, app?: string) => {
	const prefix = `${app}-quay-`;
	return app && component.startsWith(prefix)
		? component.slice(prefix.length)
		: component;
};

const commitLabel = (c: BuildCommit, app?: string) =>
	`${componentLabel(c.component, app)}@${c.commit_sha.slice(0, 7)}`;

function IssuesTable({
	issues,
	hasBuild,
	app,
	columnMgmt,
}: {
	issues: BuildTicket[];
	hasBuild: boolean;
	app?: string;
	columnMgmt: ReturnType<typeof useColumnManagement>;
}) {
	const { isColumnVisible, visibleColumns } = columnMgmt;

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
				case "priority":
					cmp =
						(priorityWeight[a.priority.toLowerCase()] ?? 5) -
						(priorityWeight[b.priority.toLowerCase()] ?? 5);
					break;
				case "status":
					cmp = a.status.localeCompare(b.status);
					break;
				case "assignee":
					cmp = a.assignee.localeCompare(b.assignee);
					break;
				case "qaContact":
					cmp = a.qa_contact.localeCompare(b.qa_contact);
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

	const visibleColumnKeys = visibleColumns.map((c) => c.key);

	const getSortParams = (columnKey: string): ThProps["sort"] => ({
		sortBy: {
			index: activeSortKey
				? visibleColumnKeys.indexOf(activeSortKey)
				: undefined,
			direction: activeSortDirection,
		},
		onSort: (_event, _index, direction) => {
			setActiveSortKey(columnKey);
			setActiveSortDirection(direction);
		},
		columnIndex: visibleColumnKeys.indexOf(columnKey),
	});

	return (
		<>
			<Table variant="compact" style={{ tableLayout: "auto" }}>
				<Thead>
					<Tr>
						{isColumnVisible("key") && (
							<Th style={{ whiteSpace: "nowrap" }}>Key</Th>
						)}
						{isColumnVisible("type") && (
							<Th sort={getSortParams("type")} style={{ whiteSpace: "nowrap" }}>
								Type
							</Th>
						)}
						{isColumnVisible("summary") && <Th>Summary</Th>}
						{isColumnVisible("priority") && (
							<Th
								sort={getSortParams("priority")}
								style={{ whiteSpace: "nowrap", minWidth: "120px" }}
							>
								Priority
							</Th>
						)}
						{isColumnVisible("status") && (
							<Th
								sort={getSortParams("status")}
								style={{ whiteSpace: "nowrap", minWidth: "110px" }}
							>
								Status
							</Th>
						)}
						{isColumnVisible("assignee") && (
							<Th
								sort={getSortParams("assignee")}
								style={{ whiteSpace: "nowrap" }}
							>
								Assignee
							</Th>
						)}
						{isColumnVisible("qaContact") && (
							<Th
								sort={getSortParams("qaContact")}
								style={{ whiteSpace: "nowrap" }}
							>
								QA Contact
							</Th>
						)}
						{isColumnVisible("target") && <Th>Target</Th>}
						{isColumnVisible("inBuild") && (
							<Th
								sort={getSortParams("inBuild")}
								style={{ whiteSpace: "nowrap" }}
							>
								In build
							</Th>
						)}
					</Tr>
				</Thead>
				<Tbody>
					{sortedIssues.map((issue) => (
						<Tr key={issue.key}>
							{isColumnVisible("key") && (
								<Td style={{ whiteSpace: "nowrap" }}>
									<a
										href={issue.link}
										target="_blank"
										rel="noopener noreferrer"
									>
										{issue.key}
									</a>
								</Td>
							)}
							{isColumnVisible("type") && <Td>{issue.issue_type}</Td>}
							{isColumnVisible("summary") && (
								<Td style={{ whiteSpace: "normal", wordBreak: "break-word" }}>
									{issue.summary}
								</Td>
							)}
							{isColumnVisible("priority") && (
								<Td>
									<PriorityLabel priority={issue.priority} />
								</Td>
							)}
							{isColumnVisible("status") && (
								<Td>
									<StatusLabel status={issue.status} />
								</Td>
							)}
							{isColumnVisible("assignee") && <Td>{issue.assignee}</Td>}
							{isColumnVisible("qaContact") && <Td>{issue.qa_contact}</Td>}
							{isColumnVisible("target") && (
								<Td>{issue.fix_version.replace(/^[a-z]+-v/, "")}</Td>
							)}
							{isColumnVisible("inBuild") && (
								<Td style={{ whiteSpace: "nowrap" }}>
									{hasBuild &&
										(issue.in_build.length === 0
											? "-"
											: issue.in_build.map((c) => (
													<div key={`${c.component}@${c.commit_sha}`}>
														<a
															href={c.commit_url}
															target="_blank"
															rel="noopener noreferrer"
															title={c.component}
														>
															{commitLabel(c, app)}
														</a>
													</div>
												)))}
								</Td>
							)}
						</Tr>
					))}
				</Tbody>
			</Table>
			<ColumnManagementModal
				appliedColumns={columnMgmt.appliedColumns}
				applyColumns={columnMgmt.applyColumns}
				isOpen={columnMgmt.isModalOpen}
				onClose={columnMgmt.closeModal}
			/>
		</>
	);
}

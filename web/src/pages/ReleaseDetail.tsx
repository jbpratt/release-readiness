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
	Label,
	MenuToggle,
	PageSection,
	Popover,
	ProgressStep,
	ProgressStepper,
	Select,
	SelectList,
	SelectOption,
	Spinner,
	Title,
} from "@patternfly/react-core";
import {
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
import { Link, useParams } from "react-router-dom";
import {
	getRelease,
	getReleaseComponents,
	getReleaseIssueSummary,
	getReleaseReadiness,
	listReleaseIssues,
} from "../api/client";
import type {
	DashboardConfig,
	IssueSummary,
	JiraIssue,
	ReadinessResponse,
	ReleaseComponent,
	ReleaseVersion,
} from "../api/types";
import GitShaLink from "../components/GitShaLink";
import PriorityLabel from "../components/PriorityLabel";
import StatusLabel from "../components/StatusLabel";
import { useCachedFetch } from "../hooks/useCachedFetch";
import {
	type ColumnDef,
	useColumnManagement,
} from "../hooks/useColumnManagement";
import { useConfig } from "../hooks/useConfig";
import { formatReleaseName, jiraIssueUrl, quayImageUrl } from "../utils/links";

export default function ReleaseDetail() {
	const { version } = useParams<{ version: string }>();
	const config = useConfig();

	const { data: release, loading: loadingRelease } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);
	const { data: releaseComponents } = useCachedFetch(
		version ? `components:${version}` : null,
		() => getReleaseComponents(version!),
	);
	const { data: issues } = useCachedFetch(
		version ? `issues:${version}` : null,
		() => listReleaseIssues(version!),
	);
	const { data: issueSummary } = useCachedFetch(
		version ? `issueSummary:${version}` : null,
		() => getReleaseIssueSummary(version!),
	);
	const { data: readinessSignal } = useCachedFetch(
		version ? `readiness:${version}` : null,
		() => getReleaseReadiness(version!),
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
	const components = releaseComponents?.components ?? null;

	return (
		<>
			<PageSection>
				<Breadcrumb>
					<BreadcrumbItem>
						<Link to="/">Releases</Link>
					</BreadcrumbItem>
					<BreadcrumbItem isActive>{displayName}</BreadcrumbItem>
				</Breadcrumb>
			</PageSection>

			<PageSection>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
					style={{ marginBottom: "1rem" }}
				>
					<FlexItem>
						<Title headingLevel="h1">{displayName}</Title>
					</FlexItem>
					{components && components.length > 0 && (
						<FlexItem>
							<Link to={`/releases/${encodeURIComponent(version!)}/snapshots`}>
								View all snapshots
							</Link>
						</FlexItem>
					)}
				</Flex>

				<ReleaseSignal
					release={release}
					readiness={readinessSignal ?? null}
					jiraBaseUrl={config?.jira_base_url}
					components={components}
					issueSummary={issueSummary ?? null}
				/>

				{components && components.length > 0 && (
					<Card isCompact style={{ marginBottom: "1rem" }}>
						<CardTitle>
							Current component images ({components.length})
						</CardTitle>
						<CardBody>
							<Table variant="compact">
								<Thead>
									<Tr>
										<Th>Component</Th>
										<Th>Image</Th>
										<Th>Git SHA</Th>
										<Th>Application</Th>
										<Th>Snapshot</Th>
										<Th>Built</Th>
									</Tr>
								</Thead>
								<Tbody>
									{components.map((c) => {
										const imgUrl = quayImageUrl(c.image);
										const digest = c.image.split("@")[1];
										const imgDisplay = digest
											? digest.replace(/^(sha256:.{12}).*/, "$1")
											: c.image;
										return (
											<Tr key={c.name}>
												<Td>{c.name}</Td>
												<Td>
													{imgUrl ? (
														<a
															href={imgUrl}
															target="_blank"
															rel="noopener noreferrer"
															title={c.image}
														>
															<code style={{ fontSize: "0.85em" }}>
																{imgDisplay}
															</code>
														</a>
													) : (
														<code
															style={{ fontSize: "0.85em" }}
															title={c.image}
														>
															{imgDisplay}
														</code>
													)}
												</Td>
												<Td>
													<GitShaLink
														component={c.name}
														sha={c.git_sha}
														gitUrl={c.git_url}
													/>
												</Td>
												<Td>{c.application}</Td>
												<Td>{c.snapshot}</Td>
												<Td>{new Date(c.created_at).toLocaleString()}</Td>
											</Tr>
										);
									})}
								</Tbody>
							</Table>
						</CardBody>
					</Card>
				)}

				{(issues ?? []).length > 0 && (
					<IssuesCard
						issues={issues ?? []}
						version={version!}
						config={config ?? undefined}
					/>
				)}
			</PageSection>
		</>
	);
}

function ReleaseSignal({
	release,
	readiness,
	jiraBaseUrl,
	components,
	issueSummary,
}: {
	release: ReleaseVersion;
	readiness: ReadinessResponse | null;
	jiraBaseUrl?: string;
	components: ReleaseComponent[] | null;
	issueSummary: IssueSummary | null;
}) {
	const dueDate = release.due_date ? new Date(release.due_date) : null;
	const releaseDate = release.release_date
		? new Date(release.release_date)
		: null;
	const targetDate = dueDate ?? releaseDate;

	const daysUntil = targetDate
		? Math.ceil((targetDate.getTime() - Date.now()) / (1000 * 60 * 60 * 24))
		: null;

	const signalColor =
		readiness?.signal === "green"
			? "green"
			: readiness?.signal === "red"
				? "red"
				: readiness?.signal === "yellow"
					? "yellow"
					: "grey";

	const ticketLink = release.release_ticket_key
		? jiraIssueUrl(
				release.release_ticket_key,
				jiraBaseUrl || "https://redhat.atlassian.net",
			)
		: null;

	const buildsReady = components !== null && components.length > 0;
	const bugsVerified =
		issueSummary !== null && issueSummary.total > 0 && issueSummary.open === 0;

	const progressItems = [
		{
			label: "Builds ready",
			done: buildsReady,
			warning: components !== null && !buildsReady,
		},
		...(issueSummary ? [{ label: "Bugs verified", done: bugsVerified }] : []),
	];

	const firstIncomplete = progressItems.findIndex((i) => !i.done);

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Release Status</CardTitle>
			<CardBody>
				<Flex justifyContent={{ default: "justifyContentSpaceEvenly" }}>
					{readiness && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Signal</div>
							<Label color={signalColor} isCompact>
								{readiness.message}
							</Label>
						</FlexItem>
					)}
					<FlexItem style={{ textAlign: "center" }}>
						<div className="rr-label">Target</div>
						<div>
							{targetDate ? targetDate.toLocaleDateString() : "TBD"}
							{daysUntil !== null && ` (${daysUntil} days)`}
						</div>
					</FlexItem>
					{release.release_ticket_key && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Ticket</div>
							<div>
								{ticketLink ? (
									<a
										href={ticketLink}
										target="_blank"
										rel="noopener noreferrer"
									>
										{release.release_ticket_key}
									</a>
								) : (
									release.release_ticket_key
								)}
							</div>
						</FlexItem>
					)}
					{release.release_ticket_assignee && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Assignee</div>
							<div>{release.release_ticket_assignee}</div>
						</FlexItem>
					)}
					{release.released && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Status</div>
							<Label color="green">Released</Label>
						</FlexItem>
					)}
				</Flex>
				<ProgressStepper isCenterAligned style={{ marginTop: "1.5rem" }}>
					{progressItems.map((item, idx) => (
						<ProgressStep
							key={item.label}
							variant={
								item.done ? "success" : item.warning ? "warning" : "pending"
							}
							isCurrent={idx === firstIncomplete}
							id={`step-${idx}`}
							titleId={`step-${idx}-title`}
							aria-label={item.label}
						>
							{item.label}
						</ProgressStep>
					))}
				</ProgressStepper>
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
];

const priorityWeight: Record<string, number> = {
	blocker: 0,
	critical: 1,
	major: 2,
	normal: 3,
	minor: 4,
	undefined: 5,
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
	issues,
	version,
	config,
}: {
	issues: JiraIssue[];
	version: string;
	config?: DashboardConfig;
}) {
	const [typeFilter, setTypeFilter] = useState<string>("All");
	const [typeSelectOpen, setTypeSelectOpen] = useState(false);
	const columnMgmt = useColumnManagement("rr-columns-issues", ISSUES_COLUMNS);

	const issueTypes = useMemo(() => {
		const types = new Set(issues.map((i) => i.issue_type));
		return ["All", ...Array.from(types).sort()];
	}, [issues]);

	const filteredIssues = useMemo(
		() =>
			typeFilter === "All"
				? issues
				: issues.filter((i) => i.issue_type === typeFilter),
		[issues, typeFilter],
	);

	const jql = buildJQL(config, version);

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<FlexItem>
						{`Linked Issues (${filteredIssues.length})`}
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
				</Flex>
			</CardTitle>
			<CardBody>
				<IssuesTable issues={filteredIssues} columnMgmt={columnMgmt} />
			</CardBody>
		</Card>
	);
}

function IssuesTable({
	issues,
	columnMgmt,
}: {
	issues: JiraIssue[];
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
			}
			return activeSortDirection === "asc" ? cmp : -cmp;
		});
	}, [issues, activeSortKey, activeSortDirection]);

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
					</Tr>
				</Thead>
				<Tbody>
					{sortedIssues.map((issue) => (
						<Tr key={issue.key}>
							{isColumnVisible("key") && (
								<Td>
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

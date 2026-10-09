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
import { Link, Navigate, useParams, useSearchParams } from "react-router-dom";
import {
	getRelease,
	getReleaseIssueSummary,
	getReleaseReadiness,
	listReleaseIssues,
} from "../api/client";
import type {
	DashboardConfig,
	IssueSummary,
	JiraIssue,
	ReadinessResponse,
	ReleaseVersion,
} from "../api/types";
import PriorityLabel from "../components/PriorityLabel";
import {
	LatestSnapshot,
	useLatestSnapshot,
} from "../components/ReleaseSnapshots";
import StatusLabel from "../components/StatusLabel";
import { useCachedFetch } from "../hooks/useCachedFetch";
import {
	type ColumnDef,
	useColumnManagement,
} from "../hooks/useColumnManagement";
import { useConfig } from "../hooks/useConfig";
import { relative } from "../utils/format";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";

export default function ReleaseDetail() {
	const { version } = useParams<{ version: string }>();
	const config = useConfig();

	const { data: release, loading: loadingRelease } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);
	const { data: issues, error: issuesError } = useCachedFetch(
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
	const latest = useLatestSnapshot(version!, release);
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
				</Flex>

				<ReleaseSignal
					release={release}
					readiness={readinessSignal ?? null}
					jiraBaseUrl={config?.jira_base_url}
					issueSummary={issueSummary ?? null}
					latest={latest}
				/>

				<LatestSnapshot
					version={version!}
					state={latest}
					issueSummary={issueSummary ?? null}
				/>

				<IssuesCard
					issues={issues}
					error={issuesError}
					version={version!}
					config={config}
				/>
			</PageSection>
		</>
	);
}

function ReleaseSignal({
	release,
	readiness,
	jiraBaseUrl,
	issueSummary,
	latest,
}: {
	release: ReleaseVersion;
	readiness: ReadinessResponse | null;
	jiraBaseUrl?: string;
	issueSummary: IssueSummary | null;
	latest: ReturnType<typeof useLatestSnapshot>;
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

	const steps = pipelineSteps(release, latest, issueSummary);
	const current = steps.findIndex((s) => s.variant !== "success");

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
					{release.release_ticket_key && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Assignee</div>
							<div>{release.release_ticket_assignee || "Unassigned"}</div>
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
					{steps.map((step, idx) => (
						<ProgressStep
							key={step.label}
							variant={step.variant}
							isCurrent={idx === current}
							description={step.description}
							id={`step-${idx}`}
							titleId={`step-${idx}-title`}
							aria-label={step.label}
						>
							{step.label}
						</ProgressStep>
					))}
				</ProgressStepper>
			</CardBody>
		</Card>
	);
}

const step = (
	label: string,
	variant: "success" | "warning" | "pending",
	description?: string,
) => ({ label, variant, description });

/** ART build -> snapshot -> bugs verified -> released, from the page's data. */
function pipelineSteps(
	release: ReleaseVersion,
	latest: ReturnType<typeof useLatestSnapshot>,
	issueSummary: IssueSummary | null,
) {
	const images = latest.detail.data?.components;
	const noArt = images?.filter((c) => !c.art).length ?? 0;
	const snapshot = latest.snapshot?.missing ? undefined : latest.snapshot;
	const loadError = latest.error ?? latest.detail.error;
	return [
		loadError
			? step("ART build", "warning", "Could not load snapshot")
			: !images?.length
				? step("ART build", "pending")
				: noArt
					? step(
							"ART build",
							"warning",
							`${noArt} of ${images.length} images have no ART build record`,
						)
					: step("ART build", "success", `All ${images.length} images`),
		latest.error
			? step("Snapshot", "warning", "Could not load snapshot")
			: latest.loading
				? step("Snapshot", "pending")
				: snapshot
					? step("Snapshot", "success", relative(snapshot.created_at))
					: step("Snapshot", "pending", "No Quay snapshot yet"),
		!issueSummary
			? step("Bugs verified", "pending")
			: issueSummary.total === 0
				? step("Bugs verified", "success", "No open issues to verify")
				: issueSummary.open > 0
					? step("Bugs verified", "warning", `${issueSummary.open} open`)
					: step(
							"Bugs verified",
							"success",
							`${issueSummary.verified} verified`,
						),
		release.released
			? step("Released", "success")
			: step("Released", "pending", "Not marked released in JIRA"),
	];
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
	error,
	version,
	config,
}: {
	issues?: JiraIssue[];
	error?: Error;
	version: string;
	config?: DashboardConfig;
}) {
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
						{hasIssues
							? `Linked Issues (${filteredIssues.length})`
							: "Linked Issues"}
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
				{hasIssues ? (
					<IssuesTable issues={filteredIssues} columnMgmt={columnMgmt} />
				) : error ? (
					error.message
				) : !issues ? (
					<Spinner size="md" />
				) : config?.jira_enabled === false ? (
					"JIRA sync is not configured on this server, so linked tickets are not shown."
				) : (
					"No tickets linked to this release."
				)}
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

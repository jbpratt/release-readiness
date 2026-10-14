import {
	Breadcrumb,
	BreadcrumbHeading,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
	ClipboardCopy,
	EmptyState,
	EmptyStateBody,
	Flex,
	FlexItem,
	FormSelect,
	FormSelectOption,
	Icon,
	Label,
	type LabelProps,
	List,
	ListItem,
	Modal,
	ModalBody,
	ModalHeader,
	PageSection,
	Popover,
	Spinner,
	Title,
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarGroup,
	ToolbarItem,
	Tooltip,
} from "@patternfly/react-core";
import {
	CopyIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ExternalLinkAltIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { type ReactNode, useMemo, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { getCandidate, getRelease, listReleaseIssues } from "../api/client";
import type {
	DashboardConfig,
	FBCCandidate,
	JiraIssue,
	KonfluxRelease,
	ReleaseCandidate,
	ReleaseVersion,
	ShipmentMR,
	Verdict,
	VerdictItem,
} from "../api/types";
import LoadError from "../components/LoadError";
import {
	ExtLink,
	external,
	Flags,
	muted,
	OperatorImages,
} from "../components/OperatorImages";
import { ShipmentLabels } from "../components/ShipmentLabels";
import { Status } from "../components/Status";
import { SyncProblems, useSyncStatus } from "../components/SyncStatusWarning";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { days, due } from "../utils/format";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	sippyRunsUrl,
	ticketsJql,
} from "../utils/links";
import {
	bundleBuilt,
	compareVersions,
	fbcGroups,
	fbcParams,
	fbcPullspecs,
	fbcsBuilt,
	freshnessLine,
	jobLabel,
	needsAttention,
	noFbcText,
	type RunPill,
	readingLine,
	releaseLine,
	type StatusLine,
	splitTickets,
	tknCommand,
	verdictEntries,
	verdictLine,
} from "../utils/releaseDetail";

/** A date and time to the minute, local unless timeZone: "Oct 9, 21:48". */
const when = (iso: string, timeZone?: string) =>
	new Date(iso).toLocaleString(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
		hourCycle: "h23",
		timeZone,
	});

export default function ReleaseDetail() {
	const { version } = useParams<{ version: string }>();
	const config = useConfig();

	const {
		data: release,
		loading: loadingRelease,
		error: releaseError,
		refetch: refetchRelease,
	} = useCachedFetch(`release:${version}`, () => getRelease(version!));
	const {
		data: tickets,
		error: ticketsError,
		refetch: refetchTickets,
	} = useCachedFetch(`issues:${version}`, () => listReleaseIssues(version!));
	const candidate = useCachedFetch(`candidate:${version}`, () =>
		getCandidate(version!),
	);

	if (loadingRelease && !release) {
		return (
			<PageSection>
				<EmptyState
					icon={Spinner}
					titleText={`Loading ${formatReleaseName(version!)}…`}
				/>
			</PageSection>
		);
	}

	// Only a 404 means the release does not exist; any other failure can be retried.
	if (!release && releaseError && !releaseError.message.startsWith("404 ")) {
		return (
			<PageSection>
				<LoadError
					title="Could not load this release"
					onRetry={() => {
						refetchRelease();
						refetchTickets();
						candidate.refetch();
					}}
				>
					The server did not return data for {formatReleaseName(version!)}. Try
					again in a moment.
				</LoadError>
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
	const fbc = candidate.data?.fbc_candidate;
	const pullspecs = fbc && fbcPullspecs(fbc);

	return (
		<PageSection>
			<Flex
				alignItems={{ default: "alignItemsCenter" }}
				style={{ marginBottom: "1rem" }}
			>
				<FlexItem>
					<Breadcrumb>
						<BreadcrumbItem>
							<Link to="/">Releases</Link>
						</BreadcrumbItem>
						<BreadcrumbHeading>{displayName}</BreadcrumbHeading>
					</Breadcrumb>
				</FlexItem>
			</Flex>

			<ReleaseStatusCard
				release={release}
				shipped={candidate.data?.shipped ?? release.released}
				shipmentMR={release.shipment_mr}
				jiraBaseUrl={config?.jira_base_url}
			/>

			<BuildsCard
				data={candidate.data}
				error={candidate.error}
				loading={candidate.loading}
				onRetry={candidate.refetch}
				version={xyz}
				konflux={konfluxUrl(
					config?.konflux_ui_url ?? "",
					config?.konflux_namespace ?? "",
					app,
				)}
				inputs={
					pullspecs &&
					!candidate.data?.shipped && (
						<ReleaseInputsButton>
							<ReleaseInputs
								app={app}
								version={xyz}
								pullspecs={pullspecs}
								bugs={
									config?.jira_enabled === false
										? undefined
										: tickets?.map((t) => t.key)
								}
								dueDate={release.due_date}
							/>
						</ReleaseInputsButton>
					)
				}
			/>

			{candidate.data?.fbc_candidate && (
				<OperatorImages d={candidate.data} version={xyz} />
			)}

			<TicketsCard
				issues={tickets}
				error={ticketsError}
				version={version!}
				config={config}
				ticketKey={release.release_ticket_key}
			/>
		</PageSection>
	);
}

/** The target date, flagged while unshipped once past or within 3 days, the ticket and its assignee. */
function ReleaseStatusCard({
	release,
	shipped,
	shipmentMR,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	shipped: boolean;
	shipmentMR: ShipmentMR | null;
	jiraBaseUrl?: string;
}) {
	const ticket = release.release_ticket_key;
	const target = release.due_date ?? release.release_date;
	const dueDate = target ? due(target, shipped) : undefined;
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
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
									Past due
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
					{shipmentMR && (
						<FlexItem style={{ textAlign: "center" }}>
							<div className="rr-label">Shipment</div>
							<ShipmentLabels mr={shipmentMR} shipped={shipped} />
						</FlexItem>
					)}
				</Flex>
			</CardBody>
		</Card>
	);
}

/** A card title with a link out on the right, hidden without an href. */
function LinkedCardTitle({
	href,
	link,
	children,
}: {
	href?: string | null;
	link?: string;
	children: ReactNode;
}) {
	return (
		<CardTitle>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsCenter" }}
			>
				<FlexItem>{children}</FlexItem>
				<Flex spaceItems={{ default: "spaceItemsSm" }}>
					{href && (
						<FlexItem>
							<Button
								component="a"
								variant="secondary"
								size="sm"
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
			</Flex>
		</CardTitle>
	);
}

/**
 * The version's release candidate, its operator FBCs, or why it has none; a
 * shipped version's is labeled, with nothing to release.
 */
function BuildsCard({
	data,
	error,
	loading,
	onRetry,
	version,
	konflux,
	inputs,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	loading: boolean;
	onRetry: () => void;
	/** The version's X.Y.Z. */
	version: string;
	/** The version's application in the Konflux UI. */
	konflux: string | null;
	/** The release command button, beside the verdict. */
	inputs?: ReactNode;
}) {
	const fbc = data?.fbc_candidate;
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={fbc ? null : konflux} link="Konflux">
				Release candidate
			</LinkedCardTitle>
			<CardBody>
				{!data && loading ? (
					<Spinner size="md" />
				) : error ? (
					<LoadError
						variant="xs"
						title="Could not load the release candidate"
						onRetry={onRetry}
					>
						Its builds, CI, and release status are unavailable right now.
					</LoadError>
				) : !data ? (
					<Spinner size="md" />
				) : fbc ? (
					<Candidate
						data={data}
						fbc={fbc}
						version={version}
						konflux={konflux}
						inputs={inputs}
					/>
				) : data.shipped ? (
					`v${version} has shipped. No FBC build data is recorded for it.`
				) : (
					noFbcText(version, data.reason)
				)}
			</CardBody>
		</Card>
	);
}

/**
 * The FBC candidate: its verdict, a failed or running prod Release and the
 * verdict's items, beside the release command; then its FBCs needing
 * attention, or all of them, each with its build, stage and CI.
 */
function Candidate({
	data,
	fbc,
	version,
	konflux,
	inputs,
}: {
	data: ReleaseCandidate;
	fbc: FBCCandidate;
	version: string;
	konflux: string | null;
	inputs?: ReactNode;
}) {
	const groups = fbcGroups(data);
	const allRows = groups.flatMap((g) => g.rows);
	const [params, setParams] = useSearchParams();
	const operator = params.get("operator") ?? "";
	const ocp = params.get("ocp") ?? "";
	const set = (key: string, value: string) =>
		setParams(
			(prev) => {
				const next = new URLSearchParams(prev);
				if (value) next.set(key, value);
				else next.delete(key);
				return next;
			},
			{ replace: true },
		);
	const operators = [...new Set(allRows.map((r) => r.operator))].sort();
	const ocps = [...new Set(allRows.map((r) => r.ocp))].sort(compareVersions);
	const rows = allRows.filter(
		(r) => (!operator || r.operator === operator) && (!ocp || r.ocp === ocp),
	);
	const attention = rows.filter(needsAttention);
	const [all, setAll] = useState(false);
	const shown = all ? rows : attention;
	const entries = verdictEntries(data.verdict);
	const read = readingLine(data.reading);
	const prod = data.candidate?.prod;
	const prodLine = prod && releaseLine(prod);
	return (
		<>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsFlexStart" }}
				gap={{ default: "gapSm" }}
			>
				<FlexItem>
					<VerdictHeader
						verdict={data.verdict}
						prod={data.candidate?.prod ?? null}
					/>
					{read && (
						<div style={line}>
							<Status status={read.status}>{read.text}</Status>
						</div>
					)}
					{prodLine && prodLine.status !== "done" && (
						<div style={line} title={prod.name}>
							<Status status={prodLine.status}>Prod: {prodLine.text}</Status>
						</div>
					)}
				</FlexItem>
				{inputs && <FlexItem>{inputs}</FlexItem>}
			</Flex>
			{entries.length > 0 && (
				<List style={line}>
					{entries.map((e) => (
						<ListItem key={`${e.level} ${e.text}`}>
							<Tooltip content={e.tip.map((t) => <div key={t}>{t}</div>)}>
								<Label isCompact tabIndex={0} status={levelStatus[e.level]}>
									{e.count > 1 ? `${e.text} (${e.count})` : e.text}
								</Label>
							</Tooltip>
							{e.on && ` ${e.on}`}
							{e.action && <span style={muted}> · Next: {e.action}</span>}
						</ListItem>
					))}
				</List>
			)}
			<Toolbar style={{ marginTop: "1rem", paddingBlock: 0 }}>
				<ToolbarContent>
					<ToolbarItem>
						<FormSelect
							value={operator}
							onChange={(_e, v) => set("operator", v)}
							aria-label="Operator"
						>
							<FormSelectOption value="" label="All operators" />
							{operators.map((o) => (
								<FormSelectOption key={o} value={o} label={o} />
							))}
						</FormSelect>
					</ToolbarItem>
					<ToolbarItem>
						<FormSelect
							value={ocp}
							onChange={(_e, v) => set("ocp", v)}
							aria-label="OCP version"
							style={{ width: "12rem" }}
						>
							<FormSelectOption value="" label="All OCP versions" />
							{ocps.map((o) => (
								<FormSelectOption key={o} value={o} label={`OCP ${o}`} />
							))}
						</FormSelect>
					</ToolbarItem>
					<ToolbarItem>
						<ToggleGroup isCompact aria-label="FBCs shown">
							<ToggleGroupItem
								text={`Needs attention (${attention.length})`}
								isSelected={!all}
								onChange={() => setAll(false)}
							/>
							<ToggleGroupItem
								text={`All (${rows.length})`}
								isSelected={all}
								onChange={() => setAll(true)}
							/>
						</ToggleGroup>
					</ToolbarItem>
					<ToolbarGroup align={{ default: "alignEnd" }}>
						<ToolbarItem>
							<Button
								component={(p) => (
									<Link
										{...p}
										to={{ pathname: "ci", search: params.toString() }}
									/>
								)}
								variant="link"
								size="sm"
							>
								CI history
							</Button>
						</ToolbarItem>
						<ToolbarItem>
							<OutLink href={sippyRunsUrl(version, fbcsBuilt(fbc))}>
								Sippy
							</OutLink>
						</ToolbarItem>
						<ToolbarItem>
							<OutLink href={konflux}>Konflux</OutLink>
						</ToolbarItem>
					</ToolbarGroup>
				</ToolbarContent>
			</Toolbar>
			{shown.length === 0 ? (
				<EmptyState
					variant="xs"
					headingLevel="h4"
					titleText={
						rows.length === 0 && (operator || ocp)
							? "No FBC matches the filters"
							: all
								? "No FBCs"
								: "No FBC needs attention"
					}
				/>
			) : (
				<Table variant="compact" aria-label="Candidate FBCs">
					<Thead>
						<Tr>
							<Th>Operator</Th>
							<Th>OCP</Th>
							<Th>Built (UTC)</Th>
							<Th>Stage</Th>
							<Th>CI</Th>
							<Th screenReaderText="FBC" />
						</Tr>
					</Thead>
					<Tbody>
						{shown.map((r) => (
							<Tr key={`${r.operator} ${r.ocp} ${r.fbc?.digest ?? ""}`}>
								<Td dataLabel="Operator">{r.operator}</Td>
								<Td dataLabel="OCP" modifier="nowrap">
									{r.ocp}
								</Td>
								<Td dataLabel="Built (UTC)" modifier="nowrap">
									{r.fbc ? (
										<ExtLink href={r.fbc.build_url || null} title={r.fbc.nvr}>
											{bundleBuilt(r.fbc.nvr) || r.fbc.nvr || "unresolved"}
										</ExtLink>
									) : (
										<span style={muted}>–</span>
									)}
								</Td>
								<Td dataLabel="Stage">
									<Flex
										gap={{ default: "gapMd" }}
										alignItems={{ default: "alignItemsCenter" }}
									>
										{r.stage ? (
											<Status status={r.stage.status}>{r.stage.text}</Status>
										) : (
											<span style={muted}>{r.fbc ? "Not staged" : "–"}</span>
										)}
										<Flags flags={r.flags} />
										{r.fbc?.release?.job_url && (
											<ExtLink href={r.fbc.release.job_url}>ART job</ExtLink>
										)}
									</Flex>
								</Td>
								<Td dataLabel="CI" modifier="nowrap">
									{r.runs.length === 0 ? (
										<span style={muted}>No runs</span>
									) : (
										<RunSummary runs={r.runs} />
									)}
								</Td>
								<Td dataLabel="FBC" modifier="fitContent">
									{r.fbc && (
										<Button
											variant="plain"
											size="sm"
											icon={<CopyIcon />}
											aria-label={`Copy the ${r.operator} OCP ${r.ocp} FBC pullspec`}
											title={r.fbc.pullspec}
											onClick={() =>
												navigator.clipboard.writeText(r.fbc?.pullspec ?? "")
											}
										/>
									)}
								</Td>
							</Tr>
						))}
					</Tbody>
				</Table>
			)}
		</>
	);
}

/** A small link button opening href in a new tab, hidden without an href. */
function OutLink({
	href,
	children,
}: {
	href: string | null;
	children: ReactNode;
}) {
	return (
		href && (
			<Button
				component="a"
				variant="link"
				size="sm"
				href={href}
				{...external}
				icon={<ExternalLinkAltIcon />}
				iconPosition="end"
			>
				{children}
			</Button>
		)
	);
}

const levelStatus: Record<VerdictItem["level"], LabelProps["status"]> = {
	blocker: "danger",
	warning: "warning",
	info: "info",
};

const runStatus: Record<string, StatusLine["status"]> = {
	success: "done",
	failure: "failed",
	error: "failed",
	pending: "running",
};
const runStatusOf = (p: RunPill) => runStatus[p.state] ?? "warning";

/** Run results as counted, worst first. */
const runWords: [StatusLine["status"], string][] = [
	["failed", "failed"],
	["running", "running"],
	["warning", "other"],
	["done", "passed"],
];
const runWordOf = (p: RunPill) =>
	runWords.find(([s]) => s === runStatusOf(p))?.[1];

/**
 * An FBC row's CI: its jobs counted by result, worst first, opening a list of
 * the jobs, each linking to its newest run in Prow. A run on another FBC of
 * the same bundle is marked "≈".
 */
function RunSummary({ runs }: { runs: RunPill[] }) {
	const counts = runWords
		.map(([s, word]) => ({
			s,
			word,
			n: runs.filter((p) => runStatusOf(p) === s).length,
		}))
		.filter((c) => c.n > 0);
	return (
		<Popover
			headerContent={`${runs.length} CI jobs`}
			bodyContent={
				<List isPlain>
					{runs.map((p) => (
						<ListItem key={`${p.job} ${p.equivalent}`}>
							<a
								href={p.prow_url}
								aria-label={`${jobLabel(p.job)} ${runWordOf(p)}`}
								title={[
									`${p.job}: ${runWordOf(p)}`,
									`Started ${when(p.started_at)}`,
									p.install_failed && "Install failed",
									p.runs > 1 && `Newest of ${p.runs} runs`,
									p.equivalent &&
										"Ran on another FBC with the same bundle; its channel graph can differ.",
								]
									.filter(Boolean)
									.join("\n")}
								{...external}
							>
								<Status status={runStatusOf(p)}>
									{p.equivalent && "≈ "}
									{jobLabel(p.job)}
								</Status>
							</a>
						</ListItem>
					))}
				</List>
			}
		>
			<Button variant="link" isInline>
				<Status status={counts[0].s}>
					{counts.map((c) => `${c.n} ${c.word}`).join(" · ")}
				</Status>
			</Button>
		</Popover>
	);
}

const warningIcon = (
	<Icon status="warning" isInline>
		<ExclamationTriangleIcon />
	</Icon>
);

/** Whether the version can ship; a shipped version's is its prod Release. */
function VerdictHeader({
	verdict,
	prod,
}: {
	verdict: Verdict;
	prod: KonfluxRelease | null;
}) {
	const head = verdictLine(verdict);
	const text =
		verdict.state === "shipped" && prod?.completion_time
			? `Shipped ${when(prod.completion_time, "UTC")} UTC`
			: head.text;
	return (
		<Status status={head.status}>
			<strong>{text}</strong> <DataFreshness />
		</Status>
	);
}

/** How fresh the verdict's data is; a stale line opens its sync problems. */
function DataFreshness() {
	const status = useSyncStatus();
	const line = status && freshnessLine(status);
	if (!line) return null;
	if (line.problems.length === 0)
		return <span style={muted}>· {line.text}</span>;
	return (
		<Popover
			headerContent="Sync problems"
			bodyContent={<SyncProblems problems={line.problems} />}
		>
			<Button variant="link" isInline icon={warningIcon}>
				{line.text}
			</Button>
		</Popover>
	);
}

/** A button opening children, the release's inputs, in a modal. */
function ReleaseInputsButton({ children }: { children: ReactNode }) {
	const [open, setOpen] = useState(false);
	return (
		<>
			<Button variant="secondary" size="sm" onClick={() => setOpen(true)}>
				Review release command
			</Button>
			<Modal
				isOpen={open}
				onClose={() => setOpen(false)}
				aria-labelledby="release-inputs-title"
				variant="large"
			>
				<ModalHeader
					title="Review release command"
					labelId="release-inputs-title"
				/>
				<ModalBody tabIndex={0}>{children}</ModalBody>
			</Modal>
		</>
	);
}

/**
 * release-from-fbc's dry-run tkn command for its one release-wide run, to
 * review and run by hand: RR starts no run.
 */
function ReleaseInputs({
	app,
	version,
	pullspecs,
	bugs,
	dueDate,
}: {
	app: string;
	/** The version's X.Y.Z. */
	version: string;
	/** The candidate's fbc-pullspecs. */
	pullspecs: string;
	/** The version's ticket keys; undefined while unknown. */
	bugs?: string[];
	dueDate?: string;
}) {
	return (
		<ClipboardCopy isReadOnly isCode isExpanded variant="expansion">
			{tknCommand(fbcParams(app, version, pullspecs, bugs ?? [], dueDate))}
		</ClipboardCopy>
	);
}

const line = { marginTop: "0.5rem" };

/**
 * The version's fixVersion issues but its release ticket, shown atop the
 * page.
 */
function TicketsCard({
	issues,
	error,
	version,
	config,
	ticketKey,
}: {
	issues?: JiraIssue[];
	error?: Error;
	version: string;
	config?: DashboardConfig;
	ticketKey?: string;
}) {
	const { work } = splitTickets(issues ?? [], ticketKey);
	const hasIssues = work.length > 0;
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
				{hasIssues && ` (${work.length})`}
			</LinkedCardTitle>
			<CardBody>
				{hasIssues ? (
					<IssuesTable issues={work} />
				) : error ? (
					error.message
				) : !issues ? (
					<Spinner size="md" />
				) : config?.jira_enabled === false ? (
					"Jira sync off."
				) : (
					<EmptyState
						variant="xs"
						headingLevel="h4"
						titleText="No linked fixVersion issues"
					/>
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

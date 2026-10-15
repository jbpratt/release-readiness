import {
	Alert,
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
	ExpandableSection,
	Flex,
	FlexItem,
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
	type PopoverProps,
	Spinner,
	Toolbar,
	ToolbarContent,
	ToolbarGroup,
	ToolbarItem,
	Tooltip,
} from "@patternfly/react-core";
import { CheckCircleIcon, ExternalLinkAltIcon } from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import {
	type CSSProperties,
	type ReactNode,
	useEffect,
	useMemo,
	useState,
} from "react";
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
import { Blockers } from "../components/Blockers";
import LoadError, { Loading, ReleaseNotFound } from "../components/LoadError";
import {
	ExtLink,
	external,
	muted,
	OperatorImages,
} from "../components/OperatorImages";
import { ShipmentLabels } from "../components/ShipmentLabels";
import { Status } from "../components/Status";
import { SyncProblems, useSyncStatus } from "../components/SyncStatusWarning";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { type Blocker, blockers, splitBlockers } from "../utils/blockers";
import { type Cell, cellProblem, headline, PROBLEMS } from "../utils/fbcMatrix";
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
	type FbcRow,
	fbcGroups,
	fbcParams,
	fbcPullspecs,
	fbcsBuilt,
	freshnessLine,
	noFbcText,
	nvrLabel,
	readingLine,
	releaseLine,
	runResult,
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

	if (!release && !releaseError) {
		return (
			<PageSection>
				<Loading title={`Loading ${formatReleaseName(version!)}…`} />
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
				<DetailBreadcrumb name={version!} />
				<ReleaseNotFound version={version!} />
			</PageSection>
		);
	}

	const app = release.konflux_application ?? "";
	const xyz = release.name.replace(/^[a-z]+-v/, "");
	const fbc = candidate.data?.fbc_candidate;
	const pullspecs = fbc && fbcPullspecs(fbc);

	return (
		<PageSection>
			<DetailBreadcrumb name={release.name} />

			<ReleaseStatusCard
				release={release}
				shipped={candidate.data?.shipped ?? release.released}
				shipmentMR={release.shipment_mr}
				jiraBaseUrl={config?.jira_base_url}
			/>

			<BuildsCard
				data={candidate.data}
				error={candidate.error}
				onRetry={candidate.refetch}
				version={xyz}
				blockers={
					candidate.data && blockers(candidate.data, release.name, app, config)
				}
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
				onRetry={refetchTickets}
				version={version!}
				config={config}
				ticketKey={release.release_ticket_key}
			/>
		</PageSection>
	);
}

function DetailBreadcrumb({ name }: { name: string }) {
	return (
		<Breadcrumb style={{ marginBottom: "1rem" }}>
			<BreadcrumbItem>
				<Link to="/">Releases</Link>
			</BreadcrumbItem>
			<BreadcrumbHeading>{formatReleaseName(name)}</BreadcrumbHeading>
		</Breadcrumb>
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
									status="danger"
									isCompact
									style={{ marginLeft: "0.5rem" }}
								>
									Past due
								</Label>
							)}
							{dueDate?.kind === "soon" && (
								<Label
									status="warning"
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
	onRetry,
	version,
	blockers,
	konflux,
	inputs,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	onRetry: () => void;
	/** The version's X.Y.Z. */
	version: string;
	/** The verdict's items as blockers to act on. */
	blockers?: Blocker[];
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
				{!data && !error ? (
					<Spinner size="md" aria-label="Loading the release candidate" />
				) : !data ? (
					<LoadError
						variant="xs"
						title="Could not load the release candidate"
						onRetry={onRetry}
					>
						Its builds, CI, and release status are unavailable right now.
					</LoadError>
				) : fbc ? (
					<Candidate
						data={data}
						fbc={fbc}
						version={version}
						blockers={blockers ?? []}
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
 * verdict's items, beside the release command; then its FBCs as a matrix.
 */
function Candidate({
	data,
	fbc,
	version,
	blockers,
	konflux,
	inputs,
}: {
	data: ReleaseCandidate;
	fbc: FBCCandidate;
	version: string;
	blockers: Blocker[];
	konflux: string | null;
	inputs?: ReactNode;
}) {
	const [params] = useSearchParams();
	const entries = verdictEntries(data.verdict);
	const read = readingLine(data.reading);
	const prod = data.candidate?.prod;
	const prodLine = prod && releaseLine(prod);
	const notReady = data.verdict.state === "not_ready";
	return (
		<>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsFlexStart" }}
				gap={{ default: "gapSm" }}
			>
				<FlexItem flex={notReady ? { default: "flex_1" } : undefined}>
					{notReady ? (
						<NotReady verdict={data.verdict} blockers={blockers} />
					) : (
						<VerdictHeader
							verdict={data.verdict}
							prod={data.candidate?.prod ?? null}
						/>
					)}
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
			{!notReady && entries.length > 0 && (
				<List style={line}>
					{entries.map((e) => (
						<ListItem key={`${e.level} ${e.text}`}>
							<Tooltip content={e.tip.map((t) => <div key={t}>{t}</div>)}>
								<Label isCompact tabIndex={0} status={levelStatus[e.level]}>
									{e.count > 1 ? `${e.text} (${e.count})` : e.text}
								</Label>
							</Tooltip>
							{e.on && ` ${e.on}`}
						</ListItem>
					))}
				</List>
			)}
			<Toolbar style={{ marginTop: "1rem", paddingBlock: 0 }}>
				<ToolbarContent>
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
			<FbcMatrix data={data} />
		</>
	);
}

// Phone width, where the matrix turns on its side.
const narrowQuery = "(max-width: 768px)";

function useNarrow() {
	const [narrow, setNarrow] = useState(() => matchMedia(narrowQuery).matches);
	useEffect(() => {
		const m = matchMedia(narrowQuery);
		const on = () => setNarrow(m.matches);
		m.addEventListener("change", on);
		return () => m.removeEventListener("change", on);
	}, []);
	return narrow;
}

/** Operators as a phone-width column header names them. */
const shortNames: Record<string, string> = {
	"quay-operator": "Quay",
	"container-security-operator": "CSO",
	"quay-bridge-operator": "QBO",
};

const cellKey = (r: FbcRow) => `${r.operator}@${r.ocp}`;

/**
 * The candidate's FBCs, operators down and OCP versions across (swapped at
 * phone width so nothing scrolls sideways), under a headline naming their
 * gaps. ?cell=<operator>@<ocp> opens that FBC's detail.
 */
function FbcMatrix({ data }: { data: ReleaseCandidate }) {
	const narrow = useNarrow();
	const [params, setParams] = useSearchParams();
	const staged = data.fbc_candidate?.source !== "stream";
	const groups = fbcGroups(data);
	const rows = groups.flatMap((g) => g.rows);
	const cells = new Map<string, Cell>(
		rows.map((r) => [cellKey(r), { row: r, problem: cellProblem(r, staged) }]),
	);
	const head = headline([...cells.values()], staged);
	const operators = groups.map((g) => g.operator);
	const ocps = [...new Set(rows.map((r) => r.ocp))].sort(compareVersions);
	const across = narrow ? operators : ocps;
	const down = narrow ? ocps : operators;
	const at = (d: string, a: string) =>
		cells.get(narrow ? `${a}@${d}` : `${d}@${a}`);
	const picked = params.get("cell");
	const select = (key: string | null) =>
		setParams(
			(prev) => {
				const next = new URLSearchParams(prev);
				if (key) next.set("cell", key);
				else next.delete("cell");
				return next;
			},
			{ replace: true },
		);
	return (
		<>
			<div style={{ display: "grid", gap: "0.25rem", marginTop: "1rem" }}>
				{head.gaps.length === 0 ? (
					<Status status="done">
						<strong>{head.count}</strong>
					</Status>
				) : (
					<>
						{head.gaps.map((g) => (
							<Status key={g.text} status={g.status}>
								<strong>{g.text}</strong>
							</Status>
						))}
						<span style={muted}>{head.count}</span>
					</>
				)}
				{!staged && (
					<span style={muted}>No stage shipment holds these FBCs yet.</span>
				)}
			</div>
			<table
				aria-label="FBCs by operator and OCP version"
				style={{
					marginTop: "1rem",
					borderCollapse: "collapse",
					width: "100%",
					tableLayout: "fixed",
				}}
			>
				<colgroup>
					<col style={{ width: narrow ? "3rem" : "13rem" }} />
				</colgroup>
				<thead>
					<tr>
						<td style={{ ...axisLabel, ...muted, textAlign: "start" }}>
							{narrow ? "OCP" : "OCP version"}
						</td>
						{across.map((a) => (
							<th
								key={a}
								scope="col"
								style={axisLabel}
								title={narrow ? a : undefined}
							>
								{narrow ? (shortNames[a] ?? a) : a}
							</th>
						))}
					</tr>
				</thead>
				<tbody>
					{down.map((d) => (
						<tr key={d} style={{ borderTop: rule }}>
							<th scope="row" style={{ ...axisLabel, textAlign: "start" }}>
								{d}
							</th>
							{across.map((a, i) => {
								const c = at(d, a);
								return (
									<td key={a} style={{ textAlign: "center", padding: "2px 0" }}>
										{c ? (
											<MatrixCell
												cell={c}
												green={staged ? "built and staged" : "built"}
												open={picked === cellKey(c.row)}
												position={
													i === 0
														? "bottom-start"
														: i === across.length - 1
															? "bottom-end"
															: "bottom"
												}
												onOpen={() => select(cellKey(c.row))}
												onClose={() => select(null)}
											/>
										) : (
											<span
												style={muted}
												title="This operator has no FBC for this OCP version"
											>
												–
											</span>
										)}
									</td>
								);
							})}
						</tr>
					))}
				</tbody>
			</table>
		</>
	);
}

const rule = "1px solid var(--pf-t--global--border--color--default)";

const axisLabel: CSSProperties = {
	padding: "0.4rem 0",
	textAlign: "center",
	fontSize: "var(--pf-t--global--font--size--sm)",
	fontWeight: "var(--pf-t--global--font--weight--body--bold)",
	whiteSpace: "nowrap",
};

/**
 * One FBC: a check when green, else its problem's icon and words; a click
 * opens its detail. An edge column's detail opens inward to stay on screen.
 */
function MatrixCell({
	cell: { row, problem },
	green,
	open,
	position,
	onOpen,
	onClose,
}: {
	cell: Cell;
	green: string;
	open: boolean;
	position: PopoverProps["position"];
	onOpen: () => void;
	onClose: () => void;
}) {
	return (
		<Popover
			isVisible={open}
			shouldOpen={onOpen}
			shouldClose={onClose}
			position={position}
			maxWidth="min(28rem, calc(100vw - 6.5rem))"
			aria-label={`${row.operator} OCP ${row.ocp}`}
			headerContent={`${row.operator} · OCP ${row.ocp}`}
			bodyContent={<FbcDetail row={row} />}
		>
			<Button
				variant="plain"
				size="sm"
				isClicked={open}
				aria-label={`${row.operator} OCP ${row.ocp}: ${problem ?? green}`}
				style={{ width: "100%", whiteSpace: "nowrap" }}
			>
				<span
					style={{
						display: "inline-flex",
						justifyContent: "center",
						fontSize: "var(--pf-t--global--font--size--sm)",
						color: "var(--pf-t--global--text--color--regular)",
					}}
				>
					{problem ? (
						<Status status={PROBLEMS[problem].status}>{problem}</Status>
					) : (
						<Icon status="success">
							<CheckCircleIcon />
						</Icon>
					)}
				</span>
			</Button>
		</Popover>
	);
}

/** One FBC, check by check; each row says what is absent when it is. */
function FbcDetail({ row }: { row: FbcRow }) {
	const f = row.fbc;
	const unread = row.flags.find((x) => x.text === "can't read");
	const older = row.flags.find((x) => x.text.startsWith("holds an older"));
	const bundle = row.images.find((i) => i.nvr === f?.head_bundle_nvr);
	const release = f?.release;
	const none = (text: string) => <span style={muted}>{text}</span>;
	const notYet = (
		<Label isCompact variant="outline">
			not in API yet
		</Label>
	);
	return (
		<dl
			style={{
				display: "grid",
				gridTemplateColumns: "max-content minmax(0, 1fr)",
				gap: "0.4rem 1rem",
				margin: 0,
			}}
		>
			<DetailLine label="Built">
				{!f ? (
					<Status status="failed">Not built</Status>
				) : !f.nvr ? (
					<Status status="warning">
						ART has no build record for this image
					</Status>
				) : (
					<>
						{bundleBuilt(f.nvr) || "Built"} UTC ·{" "}
						<ExtLink href={f.build_url || null} title={f.nvr}>
							ART build
						</ExtLink>
					</>
				)}
			</DetailLine>
			<DetailLine label="Image">
				{f ? (
					<ClipboardCopy
						isReadOnly
						variant="inline-compact"
						truncation
						hoverTip="Copy the pullspec"
						clickTip="Copied"
					>
						{f.pullspec}
					</ClipboardCopy>
				) : (
					none("No image")
				)}
			</DetailLine>
			<DetailLine label="Bundle">
				{!f ? (
					none("No bundle")
				) : unread ? (
					<Status status="failed">
						Could not read this FBC: {unread.title ?? "no detail"}
					</Status>
				) : older ? (
					<Status status="warning">
						Lists an older operator bundle than the other OCP versions (
						{older.text.replace(/^holds an older bundle \((.*)\)$/, "$1")})
					</Status>
				) : f.head_bundle_nvr ? (
					<>
						Lists the release's operator bundle,{" "}
						{bundle?.commit ? (
							<span title={f.head_bundle_nvr}>
								commit <code>{bundle.commit.slice(0, 7)}</code>
							</span>
						) : (
							nvrLabel(f.head_bundle_nvr)
						)}
					</>
				) : (
					none("Not resolved yet")
				)}
			</DetailLine>
			<DetailLine label="Staged">
				{row.stage ? (
					<Status status={row.stage.status}>
						{row.stage.text}
						{release && (
							<div style={muted}>
								{release.name}
								{release.job_url && (
									<>
										{" · "}
										<ExtLink href={release.job_url}>ART job</ExtLink>
									</>
								)}
							</div>
						)}
					</Status>
				) : (
					none(f ? "Not in a stage release" : "Nothing to stage")
				)}
			</DetailLine>
			<DetailLine label="CI">
				{row.runs.length === 0
					? none("No runs")
					: row.runs.map((p) => {
							const r = runResult(p.state);
							const link = (
								<ExtLink href={p.prow_url || null}>
									{p.equivalent ? "≈ " : ""}
									{p.job}
								</ExtLink>
							);
							return (
								<div key={`${p.job} ${p.equivalent}`}>
									{r.status ? (
										<Status status={r.status}>
											{r.text}: {link}
										</Status>
									) : (
										<>
											{r.text}: {link}
										</>
									)}
								</div>
							);
						})}
			</DetailLine>
			<DetailLine label="Enterprise Contract">{notYet}</DetailLine>
			<DetailLine label="CVE scan">{notYet}</DetailLine>
		</dl>
	);
}

function DetailLine({
	label,
	children,
}: {
	label: string;
	children: ReactNode;
}) {
	return (
		<>
			<dt style={muted}>{label}</dt>
			<dd style={{ margin: 0, minWidth: 0, overflowWrap: "anywhere" }}>
				{children}
			</dd>
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

/** A not ready verdict: its blockers in an Alert, then its warnings and notes. */
function NotReady({
	verdict,
	blockers,
}: {
	verdict: Verdict;
	blockers: Blocker[];
}) {
	const [open, setOpen] = useState(false);
	const { blocking, rest } = splitBlockers(blockers);
	return (
		<>
			<Alert
				variant="danger"
				isInline
				isExpandable={blocking.length > 0}
				title={
					<>
						{verdictLine(verdict).text}{" "}
						<span style={{ fontWeight: "normal" }}>
							<DataFreshness />
						</span>
					</>
				}
			>
				{blocking.length > 0 && <Blockers groups={blocking} />}
			</Alert>
			{rest.length > 0 && (
				<ExpandableSection
					style={line}
					toggleText={`${rest.length} warning${rest.length === 1 ? "" : "s"} and notes`}
					isExpanded={open}
					onToggle={(_e, v) => setOpen(v)}
				>
					<Blockers groups={rest} />
				</ExpandableSection>
			)}
		</>
	);
}

/** How fresh the verdict's data is; a stale label opens its sync problems. */
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
			{/* The Popover opens on click; onClick makes the Label a focusable button. */}
			<Label isCompact variant="outline" status="warning" onClick={() => {}}>
				{line.text}
			</Label>
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
	onRetry,
	version,
	config,
	ticketKey,
}: {
	issues?: JiraIssue[];
	error?: Error;
	onRetry: () => void;
	version: string;
	config?: DashboardConfig;
	ticketKey?: string;
}) {
	const work = (issues ?? []).filter((i) => i.key !== ticketKey);
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
					<LoadError
						variant="xs"
						title="Could not load tickets"
						onRetry={onRetry}
					>
						The fixVersion issues for this release are unavailable right now.
					</LoadError>
				) : !issues ? (
					<Spinner size="md" aria-label="Loading tickets" />
				) : config?.jira_enabled === false ? (
					<EmptyState
						variant="xs"
						headingLevel="h4"
						titleText="Jira sync is off"
					>
						<EmptyStateBody>
							No tickets are stored for this version.
						</EmptyStateBody>
					</EmptyState>
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

const ticketLimit = 20;

function IssuesTable({ issues }: { issues: JiraIssue[] }) {
	// Type, the second column, is the only sortable one.
	const [direction, setDirection] = useState<"asc" | "desc">();
	const [showAll, setShowAll] = useState(false);

	const sortedIssues = useMemo(() => {
		if (!direction) return issues;
		return [...issues].sort((a, b) => {
			const cmp = a.issue_type.localeCompare(b.issue_type);
			return direction === "asc" ? cmp : -cmp;
		});
	}, [issues, direction]);

	const shown = showAll ? sortedIssues : sortedIssues.slice(0, ticketLimit);

	return (
		<>
			<Table
				aria-label="Tickets"
				variant="compact"
				style={{ tableLayout: "auto" }}
			>
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
					{shown.map((issue) => (
						<Tr key={issue.key}>
							<Td dataLabel="Key" style={{ whiteSpace: "nowrap" }}>
								<a href={issue.link} {...external}>
									{issue.key}
								</a>
							</Td>
							<Td dataLabel="Type" style={{ whiteSpace: "nowrap" }}>
								{issue.issue_type}
							</Td>
							<Td
								dataLabel="Summary"
								style={{ whiteSpace: "normal", wordBreak: "break-word" }}
							>
								{issue.summary}
							</Td>
						</Tr>
					))}
				</Tbody>
			</Table>
			{issues.length > ticketLimit && (
				<Button variant="link" onClick={() => setShowAll(!showAll)}>
					{showAll ? "Show fewer" : `Show all ${issues.length} tickets`}
				</Button>
			)}
		</>
	);
}

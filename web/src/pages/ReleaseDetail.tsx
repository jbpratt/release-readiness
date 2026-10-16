import {
	Alert,
	AlertActionLink,
	Breadcrumb,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardHeader,
	CardTitle,
	ClipboardCopy,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	Drawer,
	DrawerContent,
	DrawerContentBody,
	EmptyState,
	EmptyStateBody,
	ExpandableSection,
	Flex,
	Form,
	FormGroup,
	FormHelperText,
	HelperText,
	HelperTextItem,
	Icon,
	InputGroup,
	InputGroupItem,
	Label,
	List,
	ListItem,
	Modal,
	ModalBody,
	ModalHeader,
	PageSection,
	Popover,
	type PopoverProps,
	Spinner,
	TextInput,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	CopyIcon,
	CubesIcon,
	ExclamationTriangleIcon,
	InfoCircleIcon,
	TimesCircleIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import {
	type CSSProperties,
	type ReactNode,
	useRef,
	useState,
	useSyncExternalStore,
} from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import {
	getCandidate,
	getFBCTags,
	getRelease,
	getReleaseCI,
	listReleaseIssues,
} from "../api/client";
import type {
	DashboardConfig,
	FBCBuild,
	FBCCandidate,
	FBCMember,
	JiraIssue,
	KonfluxRelease,
	ReleaseCandidate,
	ReleaseVersion,
	ShipmentMR,
} from "../api/types";
import LoadError, { Loading, ReleaseNotFound } from "../components/LoadError";
import OpenIn from "../components/OpenIn";
import {
	BundleImages,
	ExtLink,
	external,
	type Issue,
	IssueNotes,
	LEVELS,
	muted,
	ORDER,
	StageFacts,
	useNarrow,
} from "../components/OperatorImages";
import { ShipmentLabels } from "../components/ShipmentLabels";
import { Status } from "../components/Status";
import { useSyncStatus } from "../components/SyncStatusWarning";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { type Blocker, blockers } from "../utils/blockers";
import {
	type Cell,
	cellProblem,
	PROBLEMS,
	streamFbc,
} from "../utils/fbcMatrix";
import { days, due, relative } from "../utils/format";
import {
	ART_PIPELINES_HEALTH_URL,
	artPipelineUrl,
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	sippyRunsUrl,
	ticketsJql,
} from "../utils/links";
import {
	bundleBuilt,
	candidateOperators,
	compareVersions,
	type FbcRow,
	fbcGroups,
	fbcNewerTitle,
	fbcParams,
	fbcPullspecs,
	fbcsBuilt,
	fbcTagList,
	fbcTagsLead,
	fbcTagsNote,
	fbcUnstagedTitle,
	issueLists,
	type LeftOut,
	noFbcText,
	nvrLabel,
	RELEASE_PENDING,
	readingLine,
	releaseLine,
	runResult,
	sharedStage,
	ticketsByStatus,
	tknCommand,
	verdictLine,
} from "../utils/releaseDetail";
import CIDrawer from "./ReleaseCI";

/** A verdict item where it applies: a matrix cell, an operator's images, or neither. */
type Placed = Issue & { cell?: string; card?: string };

/**
 * Each verdict item at its source: the matrix cells of the OCP versions it
 * names, else its operator's images, else the whole candidate.
 */
function placeIssues(d: ReleaseCandidate, bs: Blocker[]): Placed[] {
	const cells = new Set(fbcGroups(d).flatMap((g) => g.rows.map(cellKey)));
	const cards = new Set(candidateOperators(d).map((o) => o.operator));
	return d.verdict.items.flatMap((item, k) => {
		const b = bs[k];
		const keys = b.ocps
			.map((o) => `${b.operator}@${o}`)
			.filter((c) => cells.has(c));
		if (keys.length > 0) return keys.map((cell) => ({ item, b, cell }));
		if (cards.has(b.operator)) return [{ item, b, card: b.operator }];
		return [{ item, b }];
	});
}

/** Below PatternFly's lg breakpoint the CI drawer is the page's full width. */
const fullWidthQuery = "(max-width: 991px)";
const fullWidthDrawer = () => matchMedia(fullWidthQuery).matches;
const onFullWidthDrawer = (on: () => void) => {
	const m = matchMedia(fullWidthQuery);
	m.addEventListener("change", on);
	return () => m.removeEventListener("change", on);
};

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
	const ci = useCachedFetch(`ci:${version}`, () => getReleaseCI(version!));
	const [params, setParams] = useSearchParams();
	const ciCell = params.get("ci");
	const covered = useSyncExternalStore(onFullWidthDrawer, fullWidthDrawer);

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
	const issues =
		candidate.data &&
		placeIssues(
			candidate.data,
			blockers(candidate.data, release.name, app, config),
		);

	return (
		<PageSection>
			<Drawer isExpanded={!!ciCell} isInline position="end">
				<DrawerContent
					panelContent={
						<CIDrawer
							cell={ciCell}
							builds={ci.data}
							error={ci.error}
							onRetry={ci.refetch}
							onClose={() =>
								setParams(
									(prev) => {
										const next = new URLSearchParams(prev);
										next.delete("ci");
										return next;
									},
									{ replace: true },
								)
							}
						/>
					}
				>
					{/* The open drawer covers the page below lg; hidden, it is out of the tab order too. */}
					<DrawerContentBody
						style={covered && ciCell ? { display: "none" } : undefined}
					>
						<DetailBreadcrumb name={release.name} />

						<div style={{ marginBottom: "1rem" }}>
							<Title headingLevel="h1" size="2xl">
								{formatReleaseName(release.name)}
							</Title>
							<Freshness data={candidate.data} />
						</div>

						<ReleaseFacts
							release={release}
							shipped={candidate.data?.shipped ?? release.released}
							shipmentMR={release.shipment_mr}
							jiraBaseUrl={config?.jira_base_url}
							stage={candidate.data && sharedStage(candidate.data)}
						/>

						<BuildsCard
							data={candidate.data}
							error={candidate.error}
							onRetry={candidate.refetch}
							version={xyz}
							issues={issues ?? []}
							ci={ci.data}
							konflux={konfluxUrl(
								config?.konflux_ui_url ?? "",
								config?.konflux_namespace ?? "",
								app,
							)}
							jira={
								config?.jira_project
									? jiraSearchUrl(
											ticketsJql(config.jira_project, release.name),
											config.jira_base_url,
										)
									: null
							}
							inputs={
								pullspecs &&
								!candidate.data?.shipped && (
									<ReleaseInputsButton>
										<ReleaseInputs
											app={app}
											release={release.name}
											version={xyz}
											issues={tickets}
											issuesError={ticketsError}
											onRetry={refetchTickets}
											jiraEnabled={config?.jira_enabled !== false}
											ticketKey={release.release_ticket_key}
											namespace={config?.konflux_namespace || "art-quay-tenant"}
											dueDate={release.due_date}
										/>
									</ReleaseInputsButton>
								)
							}
						/>

						<TicketsCard
							issues={tickets}
							error={ticketsError}
							onRetry={refetchTickets}
							config={config}
							ticketKey={release.release_ticket_key}
						/>
					</DrawerContentBody>
				</DrawerContent>
			</Drawer>
		</PageSection>
	);
}

function DetailBreadcrumb({ name }: { name: string }) {
	return (
		<Breadcrumb style={{ marginBottom: "1rem" }}>
			<BreadcrumbItem>
				<Link to="/">Releases</Link>
			</BreadcrumbItem>
			<BreadcrumbItem isActive>{formatReleaseName(name)}</BreadcrumbItem>
		</Breadcrumb>
	);
}

/**
 * The release's own status under its title: FBC reads and a prod Release
 * still in flight. Sync state is the masthead's.
 */
function Freshness({ data }: { data?: ReleaseCandidate }) {
	const read = data && readingLine(data.reading);
	const prod = data?.candidate?.prod;
	const prodLine = prod && releaseLine(prod);
	return (
		<Flex
			gap={{ default: "gapSm", md: "gapMd" }}
			alignItems={{ default: "alignItemsCenter" }}
			style={{
				...muted,
				fontSize: "var(--pf-t--global--font--size--sm)",
				marginTop: "0.25rem",
			}}
		>
			{read && <Status status={read.status}>{read.text}</Status>}
			{prodLine && prodLine.status !== "done" && (
				<span title={prod.name}>
					<Status status={prodLine.status}>Prod: {prodLine.text}</Status>
				</span>
			)}
		</Flex>
	);
}

/**
 * The target date, flagged while unshipped once past or within 3 days, the
 * ticket, its assignee, the shipment MR, and the stage Release every operator
 * shares: the release's facts in the page header.
 */
function ReleaseFacts({
	release,
	shipped,
	shipmentMR: mr,
	jiraBaseUrl,
	stage,
}: {
	release: ReleaseVersion;
	shipped: boolean;
	shipmentMR: ShipmentMR | null;
	jiraBaseUrl?: string;
	stage?: KonfluxRelease | null;
}) {
	const ticket = release.release_ticket_key;
	const target = release.due_date ?? release.release_date;
	const dueDate = target ? due(target, shipped) : undefined;
	return (
		<DescriptionList
			isCompact
			isAutoFit
			autoFitMinModifier={{ default: "8rem", lg: "9rem" }}
			style={{ marginBottom: "1.5rem" }}
		>
			<DescriptionListGroup>
				<DescriptionListTerm>Target</DescriptionListTerm>
				<DescriptionListDescription>
					{dueDate?.date ?? "TBD"}
					{dueDate?.kind === "past" && (
						<Label status="warning" isCompact style={{ marginLeft: "0.5rem" }}>
							Past due
						</Label>
					)}
					{dueDate?.kind === "soon" && (
						<Label status="warning" isCompact style={{ marginLeft: "0.5rem" }}>
							{dueDate.days === 0
								? "Due today"
								: `Due in ${days(dueDate.days)}`}
						</Label>
					)}
				</DescriptionListDescription>
			</DescriptionListGroup>
			{ticket && (
				<DescriptionListGroup>
					<DescriptionListTerm>Ticket</DescriptionListTerm>
					<DescriptionListDescription>
						<ExtLink
							href={jiraIssueUrl(
								ticket,
								jiraBaseUrl || "https://redhat.atlassian.net",
							)}
							icon
						>
							{ticket}
						</ExtLink>
					</DescriptionListDescription>
				</DescriptionListGroup>
			)}
			<DescriptionListGroup>
				<DescriptionListTerm>Assignee</DescriptionListTerm>
				<DescriptionListDescription>
					{release.release_ticket_assignee || "Unassigned"}
				</DescriptionListDescription>
			</DescriptionListGroup>
			{mr && (
				<DescriptionListGroup>
					<DescriptionListTerm>Shipment</DescriptionListTerm>
					<DescriptionListDescription>
						<ShipmentLabels mr={mr} shipped={shipped} />
					</DescriptionListDescription>
				</DescriptionListGroup>
			)}
			{stage && <StageFacts ri={stage} />}
		</DescriptionList>
	);
}

/**
 * The version's release candidate as its FBC matrix and bundle images, or why
 * it has none; a shipped version's is labeled, with nothing to release. Its
 * blockers are shown where they apply (whole-candidate ones above the matrix,
 * the rest at their cells and operators' images) and announced to screen
 * readers.
 */
function BuildsCard({
	data,
	error,
	onRetry,
	version,
	issues,
	ci,
	konflux,
	jira,
	inputs,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	onRetry: () => void;
	/** The version's X.Y.Z. */
	version: string;
	issues: Placed[];
	/** The version's FBC builds and their CI runs. */
	ci?: FBCBuild[];
	/** The version's application in the Konflux UI. */
	konflux: string | null;
	/** The version's tickets in Jira. */
	jira: string | null;
	/** The release command button. */
	inputs?: ReactNode;
}) {
	const fbc = data?.fbc_candidate;
	return (
		<Card style={{ marginBottom: "1.5rem" }}>
			<CardHeader
				actions={{
					actions: (
						<Flex gap={{ default: "gapSm" }}>
							{inputs}
							<OpenIn
								items={[
									{ text: "CI history", to: { search: "ci=all" } },
									{
										text: "Sippy",
										href: fbc ? sippyRunsUrl(version, fbcsBuilt(fbc)) : null,
									},
									{ text: "Konflux", href: konflux },
									{ text: "Jira", href: jira },
									{ text: "ART pipelines", href: ART_PIPELINES_HEALTH_URL },
								]}
							/>
						</Flex>
					),
					hasNoOffset: true,
				}}
			>
				<CardTitle>
					<Title headingLevel="h2" size="xl">
						Release candidate
					</Title>
				</CardTitle>
			</CardHeader>
			<CardBody>
				<div role="status" className="pf-v6-screen-reader">
					{data?.verdict.state === "not_ready" &&
						verdictLine(data.verdict).text}
				</div>
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
					<>
						<FbcMatrix data={data} fbc={fbc} issues={issues} ci={ci} />
						<BundleImages
							d={data}
							version={version}
							issues={(op) => issues.filter((i) => i.card === op)}
						/>
					</>
				) : data.shipped ? (
					<EmptyState variant="xs" headingLevel="h3" titleText="No FBC data">
						<EmptyStateBody>
							v{version} has shipped. No FBC build data is recorded for it.
						</EmptyStateBody>
					</EmptyState>
				) : data.reason === "not a concrete quay-vX.Y.Z version" ? (
					<EmptyState
						variant="xs"
						headingLevel="h3"
						titleText="No FBCs expected"
					>
						<EmptyStateBody>Only Quay versions have FBC builds.</EmptyStateBody>
					</EmptyState>
				) : (
					<EmptyState
						variant="xs"
						headingLevel="h3"
						icon={CubesIcon}
						titleText="No FBCs yet"
					>
						<EmptyStateBody>
							{data.reason && data.reason !== "no release candidate yet"
								? noFbcText(version, data.reason)
								: `The matrix fills in once ART builds an FBC for v${version}.`}
						</EmptyStateBody>
					</EmptyState>
				)}
			</CardBody>
		</Card>
	);
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
 * phone width so nothing scrolls sideways), under the items about the whole
 * candidate. ?cell=<operator>@<ocp> opens that FBC's detail.
 */
function FbcMatrix({
	data,
	fbc,
	issues,
	ci,
}: {
	data: ReleaseCandidate;
	fbc: FBCCandidate;
	issues: Placed[];
	ci?: FBCBuild[];
}) {
	const narrow = useNarrow();
	const [params, setParams] = useSearchParams();
	const staged = fbc.source !== "stream";
	const groups = fbcGroups(data);
	const rows = groups.flatMap((g) => g.rows);
	const cells = new Map<string, Cell>(
		rows.map((r) => {
			const stream = r.fbc ? null : streamFbc(data.fbc_stream, r);
			return [
				cellKey(r),
				{ row: r, stream, problem: cellProblem(r, staged, stream) },
			];
		}),
	);
	const whole = issues.filter((i) => !i.cell && !i.card);
	const unstaged = !staged && !whole.some((i) => i.item.code === "not_staged");
	const operators = groups.map((g) => g.operator);
	const ocps = [...new Set(rows.map((r) => r.ocp))].sort(compareVersions);
	const across = narrow ? operators : ocps;
	const down = narrow ? ocps : operators;
	const at = (d: string, a: string) =>
		cells.get(narrow ? `${a}@${d}` : `${d}@${a}`);
	const picked = params.get("cell");
	// CI runs on a cell's builds other than its FBC, for its detail's link.
	const others = (r: FbcRow) =>
		(ci ?? [])
			.filter(
				(b) =>
					b.operator === r.operator &&
					b.ocp === r.ocp &&
					b.digest !== r.fbc?.digest,
			)
			.reduce((n, b) => n + b.runs.length, 0);
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
			{(unstaged || whole.length > 0) && (
				<div style={{ display: "grid", gap: "0.5rem", marginBottom: "0.5rem" }}>
					{unstaged && (
						<span style={muted}>No stage shipment holds these FBCs yet.</span>
					)}
					{whole.length > 0 && <IssueNotes issues={whole} />}
				</div>
			)}
			<table
				aria-label="FBCs by operator and OCP version"
				style={{
					borderCollapse: "collapse",
					width: "100%",
					tableLayout: "fixed",
				}}
			>
				<colgroup>
					<col style={{ width: narrow ? "3rem" : "15rem" }} />
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
								const key = c && cellKey(c.row);
								return (
									<td key={a} style={{ textAlign: "center", padding: "4px" }}>
										{c && key ? (
											<MatrixCell
												cell={c}
												issues={issues.filter((x) => x.cell === key)}
												others={others(c.row)}
												green={staged ? "built and staged" : "built"}
												open={picked === key}
												position={
													i === 0
														? "bottom-start"
														: i === across.length - 1
															? "bottom-end"
															: "bottom"
												}
												onOpen={() => select(key)}
												onClose={() => select(null)}
											/>
										) : (
											<Tooltip content="This operator has no FBC for this OCP version">
												{/* biome-ignore lint/a11y/noNoninteractiveTabindex: focus shows its tooltip to keyboard users. */}
												<span style={muted} tabIndex={0}>
													<span aria-hidden="true">–</span>
													<span className="pf-v6-screen-reader">
														Not expected
													</span>
												</span>
											</Tooltip>
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
	padding: "0.75rem 0",
	textAlign: "center",
	fontSize: "var(--pf-t--global--font--size--md)",
	fontWeight: "var(--pf-t--global--font--weight--body--bold)",
	whiteSpace: "nowrap",
};

/** A matrix cell's one icon, the size of the check, by tone. */
const cellIcons = {
	danger: (
		<Icon status="danger" size="xl">
			<TimesCircleIcon />
		</Icon>
	),
	warning: (
		<Icon status="warning" size="xl">
			<ExclamationTriangleIcon />
		</Icon>
	),
	info: (
		<Icon status="info" size="xl">
			<InfoCircleIcon />
		</Icon>
	),
	running: <Icon size="xl" isInProgress />,
	success: (
		<Icon status="success" size="xl">
			<CheckCircleIcon />
		</Icon>
	),
};

/**
 * One FBC as one icon: a check when green, a red X when it is missing or
 * fails or a blocker is about it, else its problem's or item's icon; the
 * words are in its tooltip and name. A click opens its detail, led by the
 * items about it. An edge column's detail opens inward to stay on screen.
 */
function MatrixCell({
	cell: { row, stream, problem },
	issues,
	others,
	green,
	open,
	position,
	onOpen,
	onClose,
}: {
	cell: Cell;
	issues: Issue[];
	others: number;
	green: string;
	open: boolean;
	position: PopoverProps["position"];
	onOpen: () => void;
	onClose: () => void;
}) {
	const ref = useRef<HTMLButtonElement>(null);
	const first = ORDER.flatMap(
		(l) => issues.find((i) => i.item.level === l) ?? [],
	)[0];
	const top = first?.item.level;
	const status = problem && PROBLEMS[problem].status;
	const tone: keyof typeof cellIcons =
		!row.fbc || status === "failed" || top === "blocker"
			? "danger"
			: status === "running"
				? "running"
				: status === "warning" || top === "warning"
					? "warning"
					: top === "info"
						? "info"
						: "success";
	const said = problem
		? problem[0].toUpperCase() + problem.slice(1)
		: first
			? `${LEVELS[first.item.level].word}: ${first.b.what}`
			: green;
	return (
		<>
			{tone !== "success" && <Tooltip content={said} triggerRef={ref} />}
			<Popover
				isVisible={open}
				shouldOpen={onOpen}
				shouldClose={onClose}
				position={position}
				maxWidth="min(28rem, calc(100vw - 6.5rem))"
				aria-label={`${row.operator} OCP ${row.ocp}`}
				headerContent={`${row.operator} · OCP ${row.ocp}`}
				bodyContent={
					<div style={{ display: "grid", gap: "0.75rem" }}>
						{issues.length > 0 && <IssueNotes issues={issues} />}
						<FbcDetail row={row} stream={stream} others={others} />
					</div>
				}
			>
				<Button
					ref={ref}
					variant="plain"
					aria-label={`${row.operator} OCP ${row.ocp}: ${said}`}
					aria-haspopup="dialog"
					aria-expanded={open}
					style={{ width: "100%", minHeight: "2.75rem" }}
				>
					{cellIcons[tone]}
				</Button>
			</Popover>
		</>
	);
}

/** An X and the check it fails. */
function Failed({ children }: { children: ReactNode }) {
	return (
		<Flex gap={{ default: "gapSm" }}>
			<Icon status="danger" isInline>
				<TimesCircleIcon />
			</Icon>
			<span>{children}</span>
		</Flex>
	);
}

/**
 * One FBC, check by check; each row says what is absent when it is. Without
 * one, only the check it fails: Built, or Staged under the stream's build.
 */
function FbcDetail({
	row,
	stream,
	others,
}: {
	row: FbcRow;
	stream: FBCMember | null;
	others: number;
}) {
	const f = row.fbc;
	const built = f ?? stream;
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
				{!built ? (
					<Failed>Not built</Failed>
				) : !built.nvr ? (
					<Status status="warning">
						ART has no build record for this image
					</Status>
				) : (
					<>
						{bundleBuilt(built.nvr) || "Built"} UTC ·{" "}
						<ExtLink href={built.build_url || null} title={built.nvr}>
							ART build
						</ExtLink>
					</>
				)}
			</DetailLine>
			{!f ? (
				stream && (
					<DetailLine label="Staged">
						<Failed>Not staged</Failed>
					</DetailLine>
				)
			) : (
				<FbcChecks row={row} f={f} others={others} />
			)}
		</dl>
	);
}

/**
 * A built FBC's checks after Built; with CI runs on the cell's other builds,
 * a link opening them in the CI drawer.
 */
function FbcChecks({
	row,
	f,
	others,
}: {
	row: FbcRow;
	f: FBCMember;
	others: number;
}) {
	const unread = row.flags.find((x) => x.text === "can't read");
	const older = row.flags.find((x) => x.text.startsWith("holds an older"));
	const bundle = row.images.find((i) => i.nvr === f.head_bundle_nvr);
	const release = f.release;
	const none = (text: string) => <span style={muted}>{text}</span>;
	return (
		<>
			<DetailLine label="Image">
				<ClipboardCopy
					isReadOnly
					variant="inline-compact"
					truncation
					hoverTip="Copy the pullspec"
					clickTip="Copied"
				>
					{f.pullspec}
				</ClipboardCopy>
			</DetailLine>
			<DetailLine label="Bundle">
				{unread ? (
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
						{release && <div style={muted}>{release.name}</div>}
					</Status>
				) : (
					none("Not in a stage release")
				)}
			</DetailLine>
			<DetailLine label="CI">
				{row.runs.length === 0
					? none(others > 0 ? "No runs on this build" : "No runs")
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
				{others > 0 && (
					<div>
						<Link
							to={`?ci=${row.operator}@${row.ocp}`}
							style={{ display: "inline-block", padding: "4px 0" }}
						>
							{others} run{others === 1 ? "" : "s"} on other builds
						</Link>
					</div>
				)}
			</DetailLine>
		</>
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

const line = { marginTop: "0.5rem" };

/** A button opening children, the release's inputs, in a modal. */
function ReleaseInputsButton({ children }: { children: ReactNode }) {
	const [open, setOpen] = useState(false);
	return (
		<>
			<Button variant="secondary" size="sm" onClick={() => setOpen(true)}>
				Release inputs
			</Button>
			<Modal
				isOpen={open}
				onClose={() => setOpen(false)}
				aria-labelledby="release-inputs-title"
				variant="large"
			>
				<ModalHeader title="Release inputs" labelId="release-inputs-title" />
				<ModalBody tabIndex={0}>{children}</ModalBody>
			</Modal>
		</>
	);
}

/**
 * One release-from-fbc parameter in a read-only field with its own copy
 * button. Without a value, text says why and the copy button is disabled, so
 * a missing value is never copied unseen.
 */
function InputField({
	name,
	label,
	value,
	text,
	list,
	help,
}: {
	/** The parameter's name in the pipeline. */
	name: string;
	label: string;
	value?: string;
	/** What the field reads without a value. */
	text?: ReactNode;
	/** A comma-separated list: shown whole, not in one line. */
	list?: boolean;
	help?: ReactNode;
}) {
	return (
		<FormGroup
			label={
				<>
					{label} <code style={muted}>{name}</code>
				</>
			}
			fieldId={`release-input-${name}`}
		>
			{value ? (
				<ClipboardCopy
					id={`release-input-${name}`}
					isReadOnly
					isCode
					isExpanded={list}
					variant={list ? "expansion" : undefined}
					hoverTip="Copy"
					clickTip="Copied"
				>
					{value}
				</ClipboardCopy>
			) : (
				<InputGroup>
					<InputGroupItem isFill>
						<TextInput
							id={`release-input-${name}`}
							readOnly
							value=""
							placeholder={typeof text === "string" ? text : undefined}
							aria-label={label}
						/>
					</InputGroupItem>
					<InputGroupItem>
						<Button
							variant="control"
							isDisabled
							aria-label={`Copy ${label}`}
							icon={<CopyIcon />}
						/>
					</InputGroupItem>
				</InputGroup>
			)}
			{!value && typeof text !== "string" && text}
			{help && (
				<FormHelperText>
					<HelperText>
						<HelperTextItem>{help}</HelperTextItem>
					</HelperText>
				</FormHelperText>
			)}
		</FormGroup>
	);
}

/**
 * release-from-fbc's inputs for its one release-wide run, to copy into the
 * ART pipelines UI form: RR starts no run. The FBC pullspecs are the ART tags
 * of the FBCs RR staged, which the server finds by digest in ART's public
 * repository when the dialog opens.
 * The dry-run tkn command, built from the same values, is folded away below.
 */
function ReleaseInputs({
	app,
	release,
	version,
	issues,
	issuesError,
	onRetry,
	jiraEnabled,
	ticketKey,
	namespace,
	dueDate,
}: {
	app: string;
	/** The release's name, quay-vX.Y.Z. */
	release: string;
	/** The version's X.Y.Z. */
	version: string;
	/** The version's issues; undefined while unknown. */
	issues?: JiraIssue[];
	issuesError?: Error;
	onRetry: () => void;
	jiraEnabled: boolean;
	/** The release ticket's key, which is no advisory issue. */
	ticketKey?: string;
	namespace: string;
	dueDate?: string;
}) {
	const [tknOpen, setTknOpen] = useState(false);
	const jiraSync = useSyncStatus()?.sources.find((s) => s.source === "jira");
	const lists = issueLists(issues, ticketKey);
	const keys = lists?.keys ?? [];
	const fetched = useCachedFetch(
		`fbc-tags:${release}`,
		() => getFBCTags(release),
		30_000,
	);
	const tags = fetched.data && fbcTagList(fetched.data);
	const params = fbcParams(app, version, tags?.value ?? "", keys, dueDate);

	// The Jira field's text and its note, by what is known of the issues.
	let jiraText: ReactNode;
	if (!jiraEnabled) {
		jiraText = "Jira sync is off: add the issues by hand";
	} else if (!lists && issuesError) {
		jiraText = (
			<Alert
				isInline
				isPlain
				variant="danger"
				title="Could not load the issues"
				actionLinks={<AlertActionLink onClick={onRetry}>Retry</AlertActionLink>}
			/>
		);
	} else if (!lists) {
		jiraText = "Loading issues…";
	} else if (keys.length === 0) {
		jiraText = `No issue of ${version} is in ${RELEASE_PENDING}`;
	}

	// The FBC field's text and its note, by what is known of the tags.
	let tagText: ReactNode;
	if (!tags && fetched.error) {
		tagText = (
			<Alert
				isInline
				isPlain
				variant="danger"
				title="Could not load ART's tags"
				actionLinks={
					<AlertActionLink onClick={fetched.refetch}>Retry</AlertActionLink>
				}
			/>
		);
	} else if (!tags) {
		tagText = "Loading tags from ART…";
	} else if (!tags.value) {
		tagText = "Not offered until every FBC has a tag";
	}

	return (
		<>
			<p>
				Paste these into the release-from-fbc form of the ART pipelines UI (dry
				run). RR starts no run.{" "}
				<ExtLink href={artPipelineUrl(namespace)}>
					Open ART pipelines UI
				</ExtLink>
			</p>
			<Form style={line}>
				<InputField name="group" label="Group" value={params.group} />
				<InputField name="assembly" label="Assembly" value={params.assembly} />
				<InputField
					name="target-release-date"
					label="Target release date"
					value={params["target-release-date"]}
					text="The release ticket has no due date"
				/>
				<InputField name="dry-run" label="Dry run" value={params["dry-run"]} />
				<InputField
					name="jira-bugs"
					label={`Jira issues${lists ? ` (${keys.length})` : ""}`}
					value={keys.join(",")}
					text={jiraText}
					list
					help={
						jiraEnabled && (
							<>
								Only issues in {RELEASE_PENDING}, as of the last Jira sync
								{jiraSync?.last_success
									? ` ${relative(jiraSync.last_success)}`
									: ": unknown time"}
								. An issue moved since is not yet here.
							</>
						)
					}
				/>
				{lists && lists.leftOut.length > 0 && (
					<LeftOutIssues groups={lists.leftOut} />
				)}
				<InputField
					name="fbc-pullspecs"
					label={`FBC pullspecs${tags?.value ? ` (${tags.count})` : ""}`}
					value={tags?.value ?? undefined}
					text={tagText}
					list
					help={
						tags?.value && (
							<>
								{fbcTagsLead(tags)} <code>{fetched.data?.repository}</code>.
								{fbcTagsNote(tags) && ` ${fbcTagsNote(tags)}`}
							</>
						)
					}
				/>
				{tags && tags.errors.length > 0 && <TagErrors errors={tags.errors} />}
				{tags && tags.warnings.length > 0 && (
					<TagWarnings warnings={tags.warnings} />
				)}
				{tags && tags.info.length > 0 && <TagInfo info={tags.info} />}
			</Form>
			<ExpandableSection
				style={line}
				toggleText="tkn command (alternative)"
				isExpanded={tknOpen}
				onToggle={(_e, v) => setTknOpen(v)}
			>
				{(jiraEnabled && !lists) || !tags?.value ? (
					<span style={muted}>
						The tkn command waits for the issues and the FBC tags, so it never
						lacks jira-bugs or fbc-pullspecs unseen.
					</span>
				) : (
					<ClipboardCopy isReadOnly isCode isExpanded variant="expansion">
						{tknCommand(params)}
					</ClipboardCopy>
				)}
			</ExpandableSection>
		</>
	);
}

/** The FBC slots with no ART tag, which hold the pullspecs back. */
function TagErrors({ errors }: { errors: string[] }) {
	return (
		<Alert
			isInline
			variant="danger"
			title="No FBC pullspecs: an FBC has no ART tag"
		>
			<List isPlain>
				{errors.map((e) => (
					<ListItem key={e}>{e}</ListItem>
				))}
			</List>
		</Alert>
	);
}

/** The tags that are the newest build, as RR staged none; still listed. */
function TagWarnings({ warnings }: { warnings: string[] }) {
	return (
		<Alert isInline variant="warning" title={fbcUnstagedTitle(warnings.length)}>
			<List isPlain>
				{warnings.map((w) => (
					<ListItem key={w}>{w}</ListItem>
				))}
			</List>
		</Alert>
	);
}

/** The slots ART has built again since RR staged them: only information. */
function TagInfo({ info }: { info: string[] }) {
	return (
		<Alert isInline variant="info" title={fbcNewerTitle(info.length)}>
			<List isPlain>
				{info.map((i) => (
					<ListItem key={i}>{i}</ListItem>
				))}
			</List>
		</Alert>
	);
}

/** The release's issues not in jira-bugs, by status, as a warning. */
function LeftOutIssues({ groups }: { groups: LeftOut[] }) {
	const total = groups.reduce((n, g) => n + g.keys.length, 0);
	return (
		<Alert
			isInline
			variant="warning"
			title={`${total} issue${total === 1 ? "" : "s"} left out: not in ${RELEASE_PENDING}`}
		>
			<List isPlain>
				{groups.map((g) => (
					<ListItem key={g.status}>
						<strong>
							{g.status} ({g.keys.length})
						</strong>
						: {g.keys.join(", ")}
					</ListItem>
				))}
			</List>
		</Alert>
	);
}

/**
 * The version's fixVersion issues but its release ticket, shown atop the
 * page.
 */
function TicketsCard({
	issues,
	error,
	onRetry,
	config,
	ticketKey,
}: {
	issues?: JiraIssue[];
	error?: Error;
	onRetry: () => void;
	config?: DashboardConfig;
	ticketKey?: string;
}) {
	const work = (issues ?? []).filter((i) => i.key !== ticketKey);
	const hasIssues = work.length > 0;

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>
				Tickets
				{hasIssues && ` (${work.length})`}
			</CardTitle>
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

function TicketStatus({ status }: { status: string }) {
	return status === "Closed" ? (
		<span style={muted}>Closed</span>
	) : (
		<Label isCompact>{status || "No status"}</Label>
	);
}

function IssuesTable({ issues }: { issues: JiraIssue[] }) {
	const narrow = useNarrow();
	const [showClosed, setShowClosed] = useState(false);
	const { open, closed } = ticketsByStatus(issues);
	const shown = showClosed ? open.concat(closed) : open;

	return (
		<>
			<Table
				aria-label="Tickets"
				variant="compact"
				gridBreakPoint=""
				style={{ tableLayout: "auto" }}
			>
				<Thead>
					<Tr>
						<Th modifier="nowrap">{narrow ? "Ticket" : "Key"}</Th>
						{!narrow && <Th modifier="nowrap">Status</Th>}
						{!narrow && <Th modifier="nowrap">Type</Th>}
						{!narrow && <Th>Summary</Th>}
					</Tr>
				</Thead>
				<Tbody>
					{shown.map((issue) =>
						narrow ? (
							<Tr key={issue.key}>
								<Td dataLabel="Ticket">
									<Flex
										gap={{ default: "gapSm" }}
										alignItems={{ default: "alignItemsCenter" }}
									>
										<a href={issue.link} {...external}>
											{issue.key}
										</a>
										<TicketStatus status={issue.status} />
										<span style={muted}>{issue.issue_type}</span>
									</Flex>
									<div
										style={{ marginTop: "0.125rem", overflowWrap: "anywhere" }}
									>
										{issue.summary}
									</div>
								</Td>
							</Tr>
						) : (
							<Tr key={issue.key}>
								<Td dataLabel="Key" modifier="nowrap">
									<a href={issue.link} {...external}>
										{issue.key}
									</a>
								</Td>
								<Td dataLabel="Status" modifier="nowrap">
									<TicketStatus status={issue.status} />
								</Td>
								<Td dataLabel="Type" modifier="nowrap" style={muted}>
									{issue.issue_type}
								</Td>
								<Td dataLabel="Summary" style={{ overflowWrap: "anywhere" }}>
									{issue.summary}
								</Td>
							</Tr>
						),
					)}
				</Tbody>
			</Table>
			{closed.length > 0 && (
				<Button
					variant="link"
					isInline
					style={{ marginTop: "0.75rem" }}
					onClick={() => setShowClosed(!showClosed)}
				>
					{showClosed
						? "Hide closed tickets"
						: `Show ${closed.length} closed ticket${closed.length === 1 ? "" : "s"}`}
				</Button>
			)}
		</>
	);
}

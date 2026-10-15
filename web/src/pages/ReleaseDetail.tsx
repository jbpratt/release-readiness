import {
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
	Dropdown,
	DropdownItem,
	DropdownList,
	EmptyState,
	EmptyStateBody,
	Flex,
	FlexItem,
	Icon,
	Label,
	MenuToggle,
	Modal,
	ModalBody,
	ModalHeader,
	PageSection,
	Popover,
	type PopoverProps,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationTriangleIcon,
	ExternalLinkAltIcon,
	InfoCircleIcon,
	TimesCircleIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import {
	type CSSProperties,
	type ReactNode,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { Link, type To, useParams, useSearchParams } from "react-router-dom";
import { getCandidate, getRelease, listReleaseIssues } from "../api/client";
import type {
	DashboardConfig,
	FBCCandidate,
	FBCMember,
	JiraIssue,
	ReleaseCandidate,
	ReleaseVersion,
	ShipmentMR,
} from "../api/types";
import LoadError, { Loading, ReleaseNotFound } from "../components/LoadError";
import {
	ExtLink,
	external,
	type Issue,
	IssueNotes,
	LEVELS,
	muted,
	OperatorImages,
	ORDER,
} from "../components/OperatorImages";
import { ShipmentLabels } from "../components/ShipmentLabels";
import { Status } from "../components/Status";
import { SyncProblems, useSyncStatus } from "../components/SyncStatusWarning";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { type Blocker, blockers } from "../utils/blockers";
import {
	type Cell,
	cellProblem,
	PROBLEMS,
	streamFbc,
} from "../utils/fbcMatrix";
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
	candidateOperators,
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
	verdictLine,
} from "../utils/releaseDetail";

/** A verdict item where it applies: a matrix cell, an operator's card, or neither. */
type Placed = Issue & { cell?: string; card?: string };

/**
 * Each verdict item at its source: the matrix cells of the OCP versions it
 * names, else its operator's image card, else the whole candidate.
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
	const issues =
		candidate.data &&
		placeIssues(
			candidate.data,
			blockers(candidate.data, release.name, app, config),
		);

	return (
		<PageSection>
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
			/>

			<BuildsCard
				data={candidate.data}
				error={candidate.error}
				onRetry={candidate.refetch}
				version={xyz}
				issues={issues ?? []}
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
				<OperatorImages
					d={candidate.data}
					version={xyz}
					issues={(op) => (issues ?? []).filter((i) => i.card === op)}
				/>
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
			<BreadcrumbItem isActive>{formatReleaseName(name)}</BreadcrumbItem>
		</Breadcrumb>
	);
}

/**
 * How current the page's data is: when its sources last synced, the stale
 * ones (a popover with their problems), FBC reads and a prod Release still
 * in flight.
 */
function Freshness({ data }: { data?: ReleaseCandidate }) {
	const status = useSyncStatus();
	const line = status && freshnessLine(status);
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
			{line?.updated && <span>{line.updated}</span>}
			{line?.stale && (
				<Popover
					headerContent="Sync problems"
					bodyContent={<SyncProblems problems={line.problems} />}
				>
					<Button
						variant="link"
						isInline
						size="sm"
						icon={
							<Icon status="warning" isInline>
								<ExclamationTriangleIcon />
							</Icon>
						}
						style={{ fontSize: "inherit" }}
					>
						{line.stale}
					</Button>
				</Popover>
			)}
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
 * ticket, its assignee and the shipment MR: the release's facts in the page
 * header.
 */
function ReleaseFacts({
	release,
	shipped,
	shipmentMR: mr,
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
		<DescriptionList
			isCompact
			isAutoFit
			autoFitMinModifier={{ default: "8rem", lg: "12rem" }}
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
		</DescriptionList>
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
 * The version's release candidate as its FBC matrix, or why it has none; a
 * shipped version's is labeled, with nothing to release. Its blockers are
 * shown where they apply (whole-candidate ones above the matrix, the rest at
 * their cells and image cards) and announced to screen readers.
 */
function BuildsCard({
	data,
	error,
	onRetry,
	version,
	issues,
	konflux,
	inputs,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	onRetry: () => void;
	/** The version's X.Y.Z. */
	version: string;
	issues: Placed[];
	/** The version's application in the Konflux UI. */
	konflux: string | null;
	/** The release command button. */
	inputs?: ReactNode;
}) {
	const [params] = useSearchParams();
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
									{
										text: "CI history",
										to: { pathname: "ci", search: params.toString() },
									},
									{
										text: "Sippy",
										href: fbc ? sippyRunsUrl(version, fbcsBuilt(fbc)) : null,
									},
									{ text: "Konflux", href: konflux },
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
					<FbcMatrix data={data} fbc={fbc} issues={issues} />
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
 * The places to open the candidate in, behind one toggle. An item with an
 * href opens it in a new tab, one with a to is a router link, and one with
 * neither is left out.
 */
function OpenIn({
	items,
}: {
	items: { text: string; href?: string | null; to?: To }[];
}) {
	const [open, setOpen] = useState(false);
	return (
		<Dropdown
			isOpen={open}
			onSelect={() => setOpen(false)}
			onOpenChange={setOpen}
			popperProps={{ position: "right" }}
			toggle={(ref) => (
				<MenuToggle
					ref={ref}
					variant="secondary"
					size="sm"
					isExpanded={open}
					onClick={() => setOpen(!open)}
				>
					Open in
				</MenuToggle>
			)}
		>
			<DropdownList>
				{items
					.filter((i) => i.href || i.to)
					.map(({ text, href, to }) =>
						href ? (
							<DropdownItem key={text} to={href} {...external}>
								{text} {/* PF's own external icon shows only on hover. */}
								<ExternalLinkAltIcon
									style={{
										marginInlineStart: "0.25rem",
										color: "var(--pf-t--global--icon--color--subtle)",
									}}
								/>
							</DropdownItem>
						) : (
							<DropdownItem
								key={text}
								component={(p) => <Link {...p} to={to ?? ""} />}
							>
								{text}
							</DropdownItem>
						),
					)}
			</DropdownList>
		</Dropdown>
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
 * phone width so nothing scrolls sideways), under the items about the whole
 * candidate. ?cell=<operator>@<ocp> opens that FBC's detail.
 */
function FbcMatrix({
	data,
	fbc,
	issues,
}: {
	data: ReleaseCandidate;
	fbc: FBCCandidate;
	issues: Placed[];
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
	green,
	open,
	position,
	onOpen,
	onClose,
}: {
	cell: Cell;
	issues: Issue[];
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
						<FbcDetail row={row} stream={stream} />
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
function FbcDetail({ row, stream }: { row: FbcRow; stream: FBCMember | null }) {
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
				<FbcChecks row={row} f={f} />
			)}
		</dl>
	);
}

/** A built FBC's checks after Built. */
function FbcChecks({ row, f }: { row: FbcRow; f: FBCMember }) {
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

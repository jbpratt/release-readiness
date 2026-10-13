import {
	Breadcrumb,
	BreadcrumbHeading,
	BreadcrumbItem,
	Button,
	Card,
	CardBody,
	CardTitle,
	ClipboardCopy,
	clipboardCopyFunc,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	EmptyState,
	EmptyStateBody,
	ExpandableSection,
	Flex,
	FlexItem,
	FormSelect,
	FormSelectOption,
	Icon,
	Label,
	type LabelProps,
	PageSection,
	Popover,
	ProgressStep,
	type ProgressStepProps,
	ProgressStepper,
	Spinner,
	Tab,
	Tabs,
	TabTitleText,
	Title,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ExternalLinkAltIcon,
	InfoCircleIcon,
	InProgressIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { type ReactNode, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getCandidate, getRelease, listReleaseIssues } from "../api/client";
import type {
	CandidateCatalog,
	CIJob,
	DashboardConfig,
	FBCCandidate,
	ImageCompare,
	ImageRef,
	JiraIssue,
	KonfluxRelease,
	OperatorFBC,
	ReadProgress,
	ReleaseCandidate,
	ReleaseVersion,
	UnshippedBuild,
	Verdict,
	VerdictItem,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { days, due } from "../utils/format";
import {
	formatReleaseName,
	jiraIssueUrl,
	jiraSearchUrl,
	konfluxUrl,
	RELEASE_FROM_FBC_URL,
	sippyJobUrl,
	ticketsJql,
} from "../utils/links";
import {
	blockOpen,
	bundleBuilt,
	compareLine,
	componentLabel,
	FBC_PLACEHOLDER,
	fbcBuildStep,
	fbcCIStep,
	fbcParams,
	fbcPullspecs,
	fbcStageStep,
	imagesState,
	jobLabels,
	nvrLabel,
	pullspecsBlocked,
	readingLine,
	releaseLine,
	rowChips,
	runsStatus,
	type ShownOperator,
	type StatusLine,
	shipsVersion,
	shownOperators,
	slotItem,
	slotRows,
	stageText,
	streamAhead,
	tknCommand,
	unreadText,
	unshippedOf,
	verdictLine,
	whileChecking,
} from "../utils/releaseDetail";

/** A Konflux UI page of the version's application, or of application. */
type KonfluxLink = (
	kind?: "snapshots" | "releases",
	name?: string,
	application?: string,
) => string | null;

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };

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

	const { data: release, loading: loadingRelease } = useCachedFetch(
		`release:${version}`,
		() => getRelease(version!),
	);
	const { data: tickets, error: ticketsError } = useCachedFetch(
		`issues:${version}`,
		() => listReleaseIssues(version!),
	);
	const candidate = useCachedFetch(`candidate:${version}`, () =>
		getCandidate(version!),
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
	const app = release.konflux_application ?? "";
	const xyz = release.name.replace(/^[a-z]+-v/, "");
	// Every Release the candidate response names is in the version's
	// application, but its FBC catalogs'.
	const konflux: KonfluxLink = (kind, name, application) =>
		konfluxUrl(
			config?.konflux_ui_url ?? "",
			config?.konflux_namespace ?? "",
			application ?? app,
			kind,
			name,
		);

	return (
		<PageSection>
			<Breadcrumb style={{ marginBottom: "1rem" }}>
				<BreadcrumbItem>
					<Link to="/">Releases</Link>
				</BreadcrumbItem>
				<BreadcrumbHeading>{displayName}</BreadcrumbHeading>
			</Breadcrumb>

			<ReleaseStatusCard
				release={release}
				shipped={candidate.data?.shipped ?? release.released}
				jiraBaseUrl={config?.jira_base_url}
			/>

			<BuildsCard
				data={candidate.data}
				error={candidate.error}
				version={xyz}
				app={app}
				konflux={konflux}
				bugs={
					config?.jira_enabled === false
						? undefined
						: tickets?.map((t) => t.key)
				}
				dueDate={release.due_date}
			/>

			<TicketsCard
				issues={tickets}
				error={ticketsError}
				version={version!}
				config={config}
			/>
		</PageSection>
	);
}

/** The target date, flagged while unshipped once past or within 3 days, the ticket and its assignee. */
function ReleaseStatusCard({
	release,
	shipped,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	shipped: boolean;
	jiraBaseUrl?: string;
}) {
	const ticket = release.release_ticket_key;
	const target = release.due_date ?? release.release_date;
	const dueDate = target ? due(target, shipped) : undefined;
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Release Status</CardTitle>
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
									Past due date
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
				</Flex>
			</CardBody>
		</Card>
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

/**
 * The version's release candidate, its operator FBCs, or why it has none; a
 * shipped version's is labeled, with nothing to release.
 */
function BuildsCard({
	data,
	error,
	version,
	app,
	konflux,
	bugs,
	dueDate,
}: {
	data?: ReleaseCandidate;
	error?: Error;
	/** The version's X.Y.Z. */
	version: string;
	app: string;
	konflux: KonfluxLink;
	/** The version's ticket keys; undefined while unknown. */
	bugs?: string[];
	dueDate?: string;
}) {
	const fbc = data?.fbc_candidate;
	const fbcLines = (step: "build" | "stage", text: string, header?: string) => (
		<FBCLine
			catalogs={data?.catalogs ?? []}
			step={step}
			text={text}
			header={header}
			app={app}
			konflux={konflux}
		/>
	);
	const reason = data?.reason || "no FBC built or staged yet";
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<LinkedCardTitle href={konflux()} link="Konflux">
				{fbc ? `${version} release candidate` : "Release candidate"}
			</LinkedCardTitle>
			<CardBody>
				{error ? (
					error.message
				) : !data ? (
					<Spinner size="md" />
				) : fbc ? (
					<Candidate
						fbc={fbc}
						stream={data.fbc_stream}
						prod={data.candidate?.prod ?? null}
						shipped={data.shipped}
						verdict={data.verdict}
						reading={data.reading}
						unshipped={data.unshipped_builds ?? []}
						version={version}
						app={app}
						konflux={konflux}
						fbcLines={fbcLines}
						release={
							!data.shipped && (
								<ReleaseFromFBC
									params={fbcParams(
										app,
										version,
										fbcPullspecs(fbc),
										bugs ?? [],
										dueDate,
									)}
									blocked={pullspecsBlocked(fbc)}
									bugsUnknown={!bugs}
								/>
							)
						}
					/>
				) : data.shipped ? (
					"Shipped; no FBC data."
				) : (
					`${reason.charAt(0).toUpperCase()}${reason.slice(1)}.`
				)}
			</CardBody>
		</Card>
	);
}

/**
 * The FBC candidate: its verdict and how far FBC reads are; where it is, from
 * the stream's build through CI and stage to prod, the Build and Stage steps
 * opening their FBC catalogs; a summary row per operator; and each operator's
 * block. Unshipped, the Stage step's popover has release-from-fbc's params;
 * the Prod step's, its Release.
 * While FBC reads are pending no step warns or fails.
 */
function Candidate({
	fbc,
	stream,
	prod: prodRelease,
	shipped,
	verdict,
	reading,
	unshipped,
	version,
	app,
	konflux,
	fbcLines,
	release,
}: {
	fbc: FBCCandidate;
	stream: FBCCandidate | null;
	prod: KonfluxRelease | null;
	shipped: boolean;
	verdict: Verdict;
	reading: ReadProgress;
	unshipped: UnshippedBuild[];
	version: string;
	app: string;
	konflux: KonfluxLink;
	fbcLines: (
		step: "build" | "stage",
		text: string,
		header?: string,
	) => ReactNode;
	release?: ReactNode;
}) {
	const prod = prodRelease && {
		name: prodRelease.name,
		...releaseLine(prodRelease),
	};
	const checking = verdict.state === "checking";
	const built = whileChecking(
		fbcBuildStep(stream ?? fbc, reading.history_days),
		checking,
	);
	const tested = whileChecking(fbcCIStep(fbc), checking);
	const staged = whileChecking(fbcStageStep(fbc), checking);
	const read = readingLine(reading);
	const jobs = [
		...new Set(
			fbc.operators.flatMap((o) =>
				[...o.members, ...o.conflicts].flatMap((m) =>
					[...(m.ci?.exact ?? []), ...(m.ci?.equivalent ?? [])].map(
						(j) => j.job_name,
					),
				),
			),
		),
	];
	const labels = jobLabels(jobs);
	const label = (job: string) => labels[jobs.indexOf(job)];
	const shown = shownOperators(fbc, stream, shipped);
	const operators = fbc.operators.map((o) => o.operator);
	// Blocks the reader opened or closed, and their tabs; else open on a problem.
	const [opened, setOpened] = useState<Record<string, boolean>>({});
	const [tabs, setTabs] = useState<Record<string, BlockTab>>({});
	return (
		<>
			<VerdictHeader verdict={verdict} prod={prodRelease} />
			{read && (
				<div style={line}>
					<Status status={read.status}>{read.text}</Status>
				</div>
			)}
			<ProgressStepper
				isCenterAligned
				aria-label="Where the release candidate is"
				style={{ marginTop: "1rem" }}
			>
				<Step
					title="Build"
					variant={built.variant}
					description={fbcLines("build", built.text, built.fbcs)}
				/>
				<Step title="CI" variant={tested.variant} description={tested.text} />
				<Step
					title="Stage"
					variant={staged.variant}
					isCurrent={!prod && !shipped}
					description={fbcLines("stage", staged.text)}
					popoverRender={
						release
							? (ref) => (
									<Popover
										aria-label="Release from FBC"
										triggerRef={ref}
										minWidth="40rem"
										bodyContent={release}
									/>
								)
							: undefined
					}
				/>
				<Step
					title="Prod"
					variant={
						prod
							? releaseVariants[prod.status]
							: shipped
								? "success"
								: "pending"
					}
					isCurrent={!!prod || shipped}
					description={
						prod ? (
							<ExtLink href={konflux("releases", prod.name)} title={prod.name}>
								{prod.status === "failed" ? "Release failed" : prod.text}
							</ExtLink>
						) : shipped ? (
							"Shipped"
						) : (
							"Not started"
						)
					}
					popoverRender={
						prod?.status === "failed"
							? (ref) => (
									<Popover
										aria-label="Prod detail"
										headerContent="Prod"
										triggerRef={ref}
										bodyContent={
											<ExtLink
												href={konflux("releases", prod.name)}
												title={prod.name}
											>
												{prod.text}
											</ExtLink>
										}
									/>
								)
							: undefined
					}
				/>
			</ProgressStepper>
			<OperatorSummary
				fbc={fbc}
				stream={stream}
				shipped={shipped}
				items={verdict.items}
			/>
			{shown.map((s) => {
				const op = s.operator.operator;
				return (
					<OperatorBlock
						key={op}
						shown={s}
						open={opened[op] ?? blockOpen(s, verdict.items)}
						onToggle={(open) => setOpened((o) => ({ ...o, [op]: open }))}
						tab={tabs[op] ?? "fbcs"}
						onTab={(tab) => setTabs((t) => ({ ...t, [op]: tab }))}
						ahead={shipped || s.leftOut ? null : streamAhead(fbc, stream, op)}
						items={verdict.items}
						shipped={shipped}
						unshipped={unshippedOf(unshipped, operators, op)}
						label={label}
						version={version}
						app={app}
						konflux={konflux}
					/>
				);
			})}
			{fbc.unknown.length > 0 && (
				<div style={line}>
					<Status status="warning">
						{`${fbc.unknown.length} staged catalog${fbc.unknown.length === 1 ? "" : "s"} not identified: `}
						{fbc.unknown.map((m, i) => (
							<span key={m.digest}>
								{i > 0 && ", "}
								<ExtLink
									href={konflux("snapshots", m.snapshot, `fbc-${app}`)}
									title={m.pullspec}
								>
									{shortDigest(m.pullspec)}
								</ExtLink>
							</span>
						))}
					</Status>
				</div>
			)}
		</>
	);
}

const levelIcons: Record<VerdictItem["level"], ReactNode> = {
	blocker: (
		<Icon status="danger" isInline>
			<ExclamationCircleIcon />
		</Icon>
	),
	warning: (
		<Icon status="warning" isInline>
			<ExclamationTriangleIcon />
		</Icon>
	),
	info: (
		<Icon status="info" isInline>
			<InfoCircleIcon />
		</Icon>
	),
};

/**
 * Whether the version can ship, then a line per blocker, warning and info,
 * with who acts on it; a shipped version's is its prod Release.
 */
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
			? `Shipped ${when(prod.completion_time, "UTC")} UTC (image shipment ${prod.name})`
			: head.text;
	return (
		<>
			<Status status={head.status}>
				<strong>{text}</strong>
			</Status>
			{verdict.items.map((i) => (
				<Flex
					key={`${i.code} ${i.text}`}
					gap={{ default: "gapSm" }}
					flexWrap={{ default: "nowrap" }}
					style={{ marginLeft: "1.5rem" }}
				>
					<FlexItem>{levelIcons[i.level]}</FlexItem>
					<FlexItem>{i.text}</FlexItem>
					<FlexItem align={{ default: "alignRight" }} style={muted}>
						{i.owner}
					</FlexItem>
				</Flex>
			))}
		</>
	);
}

/**
 * A row per operator: the version and bundle its FBCs ship and where its
 * images are.
 */
function OperatorSummary({
	fbc,
	stream,
	shipped,
	items,
}: {
	fbc: FBCCandidate;
	stream: FBCCandidate | null;
	shipped: boolean;
	items: VerdictItem[];
}) {
	const flagged = (operator: string, code: string) =>
		items.some((i) => i.operator === operator && i.code === code);
	return (
		<Table
			variant="compact"
			aria-label="Operators"
			style={{ marginTop: "1rem" }}
		>
			<Thead>
				<Tr>
					<Th>Operator</Th>
					<Th>Ships</Th>
					<Th>Bundle</Th>
					<Th>Images</Th>
				</Tr>
			</Thead>
			<Tbody>
				{shownOperators(fbc, stream, shipped).map(({ operator: o }) => {
					const ships = shipsVersion(o);
					const images = imagesState(o, shipped);
					const unread = unreadText(items, o.operator) ?? "reading…";
					return (
						<Tr key={o.operator}>
							<Td>{o.operator}</Td>
							<Td modifier="nowrap">
								{ships ? (
									<>
										{ships}{" "}
										{flagged(o.operator, "csv_mismatch") && levelIcons.blocker}
									</>
								) : (
									<span style={muted}>{unread}</span>
								)}
							</Td>
							<Td modifier="nowrap">
								<span title={o.head_bundle_nvr}>
									{bundleBuilt(o.head_bundle_nvr)}
								</span>
							</Td>
							<Td modifier="nowrap">
								{images || <span style={muted}>{unread}</span>}
							</Td>
						</Tr>
					);
				})}
			</Tbody>
		</Table>
	);
}

/** An FBC NVR without its operator: quay-operator-fbc-3.17.6-1.ocp4.14 is 3.17.6-1.ocp4.14. */
const fbcLabel = (nvr: string) => nvr.replace(/^.*-fbc-/, "");

type BlockTab = "fbcs" | "images";

// An operator's images, by where they are.
const imagesTexts: Record<string, string> = {
	"on stage": "images on stage",
	"in prod": "images in prod",
	"not on stage": "images not on stage: CI can't install",
	"push to stage failed": "image push to stage failed",
};

/** The commit an ART NVR's ".g<sha>" names. */
const nvrCommit = (nvr: string) => nvr.match(/\.g([0-9a-f]{7,})\./)?.[1] ?? "";

/**
 * An operator's block, open on a problem until the reader toggles it: the
 * version, bundle and channel its FBCs ship, where they are, a newer stream
 * build not on stage; then its FBCs by OCP version, with exception chips and
 * CI where runs exist, and its images, compared with the previous z or that
 * newer build. A left-out operator's are the stream's FBCs to add.
 */
function OperatorBlock({
	shown: { operator: o, leftOut },
	open,
	onToggle,
	tab,
	onTab,
	ahead,
	items,
	shipped,
	unshipped,
	label,
	version,
	app,
	konflux,
}: {
	shown: ShownOperator;
	open: boolean;
	onToggle: (open: boolean) => void;
	tab: BlockTab;
	onTab: (tab: BlockTab) => void;
	/** The stream's newer head bundle, when the stage candidate is behind it. */
	ahead: string | null;
	items: VerdictItem[];
	shipped: boolean;
	unshipped: UnshippedBuild[];
	label: (job: string) => string;
	version: string;
	app: string;
	konflux: KonfluxLink;
}) {
	const [compare, setCompare] = useState("");
	const rows = slotRows(o);
	const fbcs = rows.filter((r) => r.fbc).length;
	const nvr = o.head_bundle_nvr;
	const unread = unreadText(items, o.operator);
	const head = nvr
		? [shipsVersion(o), `bundle ${bundleBuilt(nvr)}`, nvrCommit(nvr), o.channel]
				.filter(Boolean)
				.join(" · ")
		: (unread ?? "reading…");
	const images = imagesTexts[imagesState(o, shipped)];
	const status = leftOut
		? `Not in the stage shipment: add these ${fbcs} FBCs`
		: [shipped ? "" : stageText(o, items), images].filter(Boolean).join(" · ");
	const newer = o.compare.find((c) => c.kind === "newer");
	const change = newer && compareLine(newer);
	const ci = rows.some(
		(r) =>
			(r.fbc?.ci?.exact.length ?? 0) + (r.fbc?.ci?.equivalent.length ?? 0) > 0,
	);
	return (
		<ExpandableSection
			isExpanded={open}
			onToggle={(_e, v) => onToggle(v)}
			toggleContent={
				<span>
					<strong>{o.operator}</strong>{" "}
					<span style={muted} title={nvr || undefined}>
						{head}
					</span>
				</span>
			}
			style={{ marginTop: "1rem" }}
		>
			{status && (
				<div>
					{status.charAt(0).toUpperCase()}
					{status.slice(1)}
				</div>
			)}
			{ahead && (
				<Flex gap={{ default: "gapSm" }} flexWrap={{ default: "nowrap" }}>
					<FlexItem>{levelIcons[change?.level ?? "info"]}</FlexItem>
					<FlexItem>
						Newer build not on stage:{" "}
						<span title={ahead}>bundle {bundleBuilt(ahead)}</span>
						{change && ` (${change.text})`}{" "}
						{newer && (
							<Button
								variant="link"
								isInline
								onClick={() => {
									setCompare("newer");
									onTab("images");
								}}
							>
								Compare
							</Button>
						)}
					</FlexItem>
				</Flex>
			)}
			<Tabs
				activeKey={tab}
				onSelect={(_e, k) => onTab(k as BlockTab)}
				aria-label={`${o.operator} FBCs and images`}
			>
				<Tab eventKey="fbcs" title={<TabTitleText>FBCs {fbcs}</TabTitleText>}>
					<Table
						variant="compact"
						aria-label={`${o.operator} FBCs by OCP version`}
					>
						<Thead>
							<Tr>
								<Th modifier="fitContent">OCP</Th>
								<Th>FBC</Th>
								<Th>Digest</Th>
								{ci && <Th screenReaderText="CI" />}
							</Tr>
						</Thead>
						<Tbody>
							{rows.map((r) => (
								<Tr key={`${r.ocp} ${r.fbc?.digest ?? ""}`}>
									<Td modifier="nowrap">{r.ocp}</Td>
									<Td>
										{r.fbc && (
											<ExtLink href={r.fbc.build_url || null} title={r.fbc.nvr}>
												{r.fbc.nvr
													? fbcLabel(r.fbc.nvr)
													: shortDigest(r.fbc.pullspec)}
											</ExtLink>
										)}
									</Td>
									<Td modifier="nowrap">
										{r.fbc && (
											<ClipboardCopy
												variant="inline-compact"
												isCode
												onCopy={(e) =>
													clipboardCopyFunc(e, r.fbc?.pullspec ?? "")
												}
											>
												{shortDigest(r.fbc.pullspec)}
											</ClipboardCopy>
										)}
										{rowChips(r, {
											shipped,
											unreadable: slotItem(
												items,
												"read_failed",
												o.operator,
												r.ocp,
											)?.text,
										}).map((c) => (
											<Label
												key={c.text}
												isCompact
												color={c.color}
												style={{ marginLeft: "0.5rem" }}
											>
												<span title={c.title}>{c.text}</span>
											</Label>
										))}
									</Td>
									{ci && (
										<Td>
											<Flex gap={{ default: "gapXs" }}>
												{(r.fbc?.ci?.exact.length ?? 0) > 0 && (
													<JobPills
														jobs={r.fbc?.ci?.exact ?? []}
														label={label}
														version={version}
													/>
												)}
												{(r.fbc?.ci?.equivalent.length ?? 0) > 0 && (
													<JobPills
														jobs={r.fbc?.ci?.equivalent ?? []}
														label={label}
														version={version}
														equivalent
													/>
												)}
											</Flex>
										</Td>
									)}
								</Tr>
							))}
						</Tbody>
					</Table>
				</Tab>
				<Tab
					eventKey="images"
					title={
						<TabTitleText>Images {uniqueImages(o).length || null}</TabTitleText>
					}
				>
					<OperatorImages
						operator={o}
						compare={o.compare.find((c) => c.kind === compare) ?? null}
						onCompare={setCompare}
						unread={unread ?? "Reading FBC contents…"}
					/>
					{!shipped && (
						<UnshippedTable builds={unshipped} app={app} konflux={konflux} />
					)}
				</Tab>
			</Tabs>
		</ExpandableSection>
	);
}

/** An operator's images, each once: opm can list the bundle image twice. */
const uniqueImages = (o: OperatorFBC) =>
	o.images.filter(
		(i, k) =>
			o.images.findIndex((x) => x.name === i.name && x.digest === i.digest) ===
			k,
	);

/** An image of a compared bundle: its build and commit, else its digest. */
function ImageRefCell({ image }: { image: ImageRef | null }) {
	if (!image) return <span style={muted}>–</span>;
	return (
		<span title={image.nvr || image.digest}>
			{image.nvr
				? `${bundleBuilt(image.nvr) || nvrLabel(image.nvr)} ${image.commit.slice(0, 7)}`
				: image.digest.slice(7, 19)}
		</span>
	);
}

/** A compared image's repository without its registry. */
const repoName = (image: ImageRef | null) =>
	image?.repository.split("/").slice(1).join("/") ?? "";

// How a compared image changed: a rebuild from the same source is info,
// anything else may change behaviour.
const changeColors: Record<string, LabelProps["color"]> = {
	rebuild: "blue",
	"new source": "orange",
	"new build": "orange",
	renamed: "orange",
	added: "orange",
	removed: "orange",
};

/**
 * An operator's images, the relatedImages of its head bundle, each once with
 * its build, commit, digest and whether stage and prod hold it; or, compared,
 * each image's change against the previous z or a newer build. Under them,
 * the OCP versions shipping another build and an image shipment's other
 * bundle.
 */
function OperatorImages({
	operator: o,
	compare,
	onCompare,
	unread,
}: {
	operator: OperatorFBC;
	compare: ImageCompare | null;
	onCompare: (kind: string) => void;
	/** What the tab says without images. */
	unread: string;
}) {
	if (o.images.length === 0)
		return <div style={{ ...line, ...muted }}>{unread}</div>;
	const images = uniqueImages(o);
	const other = o.conflicts.filter((m) => m.head_bundle_nvr).map((m) => m.ocp);
	const check = o.image_check;
	// An external's unset stage or prod is untracked, not "not shipped".
	const yes = (on: boolean, external: boolean) =>
		on ? "yes" : external ? "" : "–";
	return (
		<>
			{o.compare.length > 0 && (
				<div style={{ ...line, width: "16rem", marginLeft: "auto" }}>
					<FormSelect
						value={compare?.kind ?? ""}
						onChange={(_e, v) => onCompare(v)}
						aria-label={`Compare ${o.operator} images`}
					>
						<FormSelectOption value="" label="This bundle's images" />
						{o.compare.map((c) => (
							<FormSelectOption
								key={c.kind}
								value={c.kind}
								label={
									c.kind === "previous" ? `vs ${c.label}` : "vs newer build"
								}
							/>
						))}
					</FormSelect>
				</div>
			)}
			{compare ? (
				<Table variant="compact" aria-label={`${o.operator} images compared`}>
					<Thead>
						<Tr>
							<Th>Image</Th>
							<Th>{compare.kind === "previous" ? compare.label : "staged"}</Th>
							<Th>{compare.kind === "previous" ? "this bundle" : "newer"}</Th>
							<Th>Change</Th>
						</Tr>
					</Thead>
					<Tbody>
						{compare.rows.map((r) => (
							<Tr key={`${r.name} ${r.from?.digest} ${r.to?.digest}`}>
								<Td>{r.name || "bundle"}</Td>
								<Td modifier="nowrap">
									<ImageRefCell image={r.from} />
								</Td>
								<Td modifier="nowrap">
									<ImageRefCell image={r.to} />
								</Td>
								<Td>
									{changeColors[r.change] ? (
										<Label isCompact color={changeColors[r.change]}>
											{r.change}
										</Label>
									) : (
										<span style={muted}>{r.change}</span>
									)}
									{r.change === "renamed" && (
										<span style={{ ...muted, marginLeft: "0.5rem" }}>
											{repoName(r.from)} → {repoName(r.to)}
										</span>
									)}
								</Td>
							</Tr>
						))}
					</Tbody>
				</Table>
			) : (
				<Table variant="compact" aria-label={`${o.operator} images`}>
					<Thead>
						<Tr>
							<Th>Image</Th>
							<Th>Build</Th>
							<Th>Commit</Th>
							<Th>Digest</Th>
							<Th>Stage</Th>
							<Th>Prod</Th>
						</Tr>
					</Thead>
					<Tbody>
						{images.map((i) => (
							<Tr key={`${i.name} ${i.digest}`}>
								<Td>{i.name || "bundle"}</Td>
								<Td modifier="nowrap">
									{i.nvr ? (
										<ExtLink href={i.build_url || null} title={i.nvr}>
											{bundleBuilt(i.nvr) || nvrLabel(i.nvr)}
										</ExtLink>
									) : (
										<span style={muted}>external</span>
									)}
								</Td>
								<Td>{i.commit.slice(0, 7)}</Td>
								<Td modifier="nowrap">
									<ClipboardCopy
										variant="inline-compact"
										isCode
										onCopy={(e) => clipboardCopyFunc(e, i.image)}
									>
										{shortDigest(i.image)}
									</ClipboardCopy>
								</Td>
								<Td>{yes(i.stage, !i.nvr)}</Td>
								<Td>{yes(i.prod, !i.nvr)}</Td>
							</Tr>
						))}
					</Tbody>
				</Table>
			)}
			<div style={muted}>
				{other.length > 0 &&
					`OCP ${other.join(", ")} ${other.length === 1 ? "ships" : "ship"} a different build. `}
				{check.state === "differs" &&
					`Image shipment has bundle ${bundleBuilt(check.bundle_nvr)}.`}
			</div>
		</>
	);
}

/**
 * The images of an operator's components the candidate's image shipment
 * leaves out, each with its stream build and ART build, copying its pullspec.
 * ART stages them by another path, so they are not "not staged".
 */
function UnshippedTable({
	builds,
	app,
	konflux,
}: {
	builds: UnshippedBuild[];
	app: string;
	konflux: KonfluxLink;
}) {
	if (builds.length === 0) return null;
	return (
		<>
			<Title headingLevel="h4" size="md" style={{ marginTop: "1rem" }}>
				Not in the image shipment
			</Title>
			<Table variant="compact">
				<Thead>
					<Tr>
						<Th>Component</Th>
						<Th>Build</Th>
						<Th>Pullspec</Th>
					</Tr>
				</Thead>
				<Tbody>
					{builds.map((b) => (
						<Tr key={b.name}>
							<Td>
								<ExtLink
									href={konflux("snapshots", b.snapshot)}
									title={b.snapshot}
								>
									{componentLabel(b.name, app)}
								</ExtLink>
							</Td>
							<Td>
								<ExtLink href={b.build_url || null} title={b.nvr}>
									{nvrLabel(b.nvr)}
								</ExtLink>
							</Td>
							<Td modifier="nowrap">
								<ClipboardCopy
									variant="inline-compact"
									isCode
									onCopy={(e) => clipboardCopyFunc(e, b.image)}
								>
									{shortDigest(b.image)}
								</ClipboardCopy>
							</Td>
						</Tr>
					))}
				</Tbody>
			</Table>
		</>
	);
}

/**
 * release-from-fbc's params, a dry-run tkn command and the Pipeline's console
 * page, to review and run by hand: RR starts no run.
 */
function ReleaseFromFBC({
	params,
	blocked,
	bugsUnknown,
}: {
	params: Record<string, string>;
	/** Why there are no fbc-pullspecs to copy. */
	blocked: string;
	/** The tickets did not load, so jira-bugs is unknown, not empty. */
	bugsUnknown: boolean;
}) {
	const pullspecs = params["fbc-pullspecs"];
	const row = (term: string, value: ReactNode, hint?: string) => (
		<DescriptionListGroup>
			<DescriptionListTerm>{term}</DescriptionListTerm>
			<DescriptionListDescription>
				{value} {hint && <span style={muted}>({hint})</span>}
			</DescriptionListDescription>
		</DescriptionListGroup>
	);
	return (
		<>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsCenter" }}
			>
				<Title headingLevel="h4" size="md">
					Release from FBC
				</Title>
				<Button
					component="a"
					variant="link"
					isInline
					href={RELEASE_FROM_FBC_URL}
					{...external}
					icon={<ExternalLinkAltIcon />}
					iconPosition="end"
				>
					Pipeline
				</Button>
			</Flex>
			<DescriptionList
				isHorizontal
				isCompact
				horizontalTermWidthModifier={{ default: "12rem" }}
				style={line}
			>
				{row("group", params.group)}
				{row("assembly", params.assembly)}
				{row(
					"fbc-pullspecs",
					pullspecs === FBC_PLACEHOLDER ? (
						<span style={muted}>not available: {blocked}</span>
					) : (
						<ClipboardCopy variant="inline-compact" truncation>
							{pullspecs}
						</ClipboardCopy>
					),
				)}
				{row(
					"jira-bugs",
					params["jira-bugs"] ?? (bugsUnknown ? "unavailable" : "none"),
					"review before running",
				)}
				{row(
					"target-release-date",
					params["target-release-date"] ?? "none",
					"hint",
				)}
			</DescriptionList>
			<ClipboardCopy variant="inline-compact" isCode isBlock style={line}>
				{tknCommand(params)}
			</ClipboardCopy>
		</>
	);
}

/** An image's digest to 12 hex digits: "sha256:0123456789ab". */
const shortDigest = (image: string) =>
	image.slice(image.indexOf("@") + 1).slice(0, 19);

/**
 * A step's text, opening the catalogs it counts, each with its Snapshot,
 * pullspec and Release: at Build all of them, at Stage the staged ones.
 */
function FBCLine({
	catalogs,
	step,
	text,
	header = "FBC catalogs",
	app,
	konflux,
}: {
	catalogs: CandidateCatalog[];
	step: "build" | "stage";
	text: string;
	header?: string;
	app: string;
	konflux: KonfluxLink;
}) {
	const shown =
		step === "stage" ? catalogs.filter((c) => c.state === "staged") : catalogs;
	if (shown.length === 0) return text;
	const fbcApp = `fbc-${app}`;
	return (
		<div>
			<Popover
				aria-label="FBC catalogs"
				headerContent={header}
				minWidth="48rem"
				bodyContent={
					<div style={{ maxHeight: "60vh", overflowY: "auto" }}>
						<Table variant="compact">
							<Thead>
								<Tr>
									<Th>Catalog</Th>
									<Th>Pullspec</Th>
									<Th>Release</Th>
								</Tr>
							</Thead>
							<Tbody>
								{shown.map((c) => (
									<Tr key={c.pullspec}>
										<Td>
											<ExtLink
												href={konflux("snapshots", c.snapshot, fbcApp)}
												title={c.nvr || c.snapshot}
											>
												{componentLabel(c.component, fbcApp)}
												{c.ocp && ` · OCP ${c.ocp}`}
											</ExtLink>{" "}
											<Label
												isCompact
												color={c.state === "staged" ? "green" : "grey"}
											>
												{c.state}
											</Label>
										</Td>
										<Td modifier="nowrap">
											<ClipboardCopy
												variant="inline-compact"
												isCode
												onCopy={(e) => clipboardCopyFunc(e, c.pullspec)}
											>
												{shortDigest(c.pullspec)}
											</ClipboardCopy>
										</Td>
										<Td>
											{c.release ? (
												<>
													<ExtLink
														href={konflux("releases", c.release.name, fbcApp)}
														title={c.release.name}
													>
														{releaseLine(c.release).text}
													</ExtLink>
													<div style={muted}>{c.release_plan}</div>
												</>
											) : (
												<span style={muted}>
													{c.state === "staged"
														? "No Release yet"
														: "Not staged"}
												</span>
											)}
										</Td>
									</Tr>
								))}
							</Tbody>
						</Table>
					</div>
				}
			>
				<Button variant="link" isInline>
					{text}
				</Button>
			</Popover>
		</div>
	);
}

const line = { marginTop: "0.5rem" };

// What a step's variant means, read out after its title: only its icon shows it.
const stepStates = {
	success: "done",
	warning: "warning",
	danger: "failed",
	info: "in progress",
	pending: "not started",
} as const;

const releaseVariants: Record<StatusLine["status"], keyof typeof stepStates> = {
	done: "success",
	failed: "danger",
	running: "info",
	warning: "warning",
};

/** A step of the candidate's stepper, read out as "<title>: <state>". */
function Step({
	title,
	variant,
	...props
}: Pick<ProgressStepProps, "isCurrent" | "description" | "popoverRender"> & {
	title: string;
	variant: keyof typeof stepStates;
}) {
	const id = `candidate-${title.toLowerCase()}`;
	return (
		<ProgressStep
			id={id}
			titleId={`${id}-title`}
			variant={variant}
			aria-label={`${title}: ${stepStates[variant]}`}
			{...props}
		>
			{title}
		</ProgressStep>
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
	running: (
		<Icon status="info" isInline>
			<InProgressIcon />
		</Icon>
	),
	warning: (
		<Icon status="warning" isInline>
			<ExclamationTriangleIcon />
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
			<FlexItem>{children}</FlexItem>
		</Flex>
	);
}

// Prow job states as the OCP release controller words them.
const ciStates: Record<string, string> = {
	success: "Succeeded",
	failure: "Failed",
	error: "Error",
};

const runState = (state: string) =>
	ciStates[state] ?? state.charAt(0).toUpperCase() + state.slice(1);

// Job statuses, failures first.
const jobRank = { danger: 0, warning: 1, success: 2, none: 3 } as const;

const jobStatus = (j: CIJob): keyof typeof jobRank =>
	runsStatus(j.runs.map((r) => r.state)) ?? "none";

/**
 * A pill per job, failures first, opening its Sippy page; its runs, each
 * opening it in Prow, on hover. Passing jobs fold into one "N passed" toggle.
 * Runs on an equivalent FBC are marked "≈".
 */
function JobPills({
	jobs,
	label,
	version,
	equivalent,
}: {
	jobs: CIJob[];
	label: (job: string) => string;
	/** The version's X.Y.Z. */
	version: string;
	/** The runs installed another FBC of this OCP version on the same bundle. */
	equivalent?: boolean;
}) {
	const mark = equivalent ? "≈ " : "";
	const [showPassed, setShowPassed] = useState(false);
	const sorted = [...jobs].sort(
		(a, b) =>
			jobRank[jobStatus(a)] - jobRank[jobStatus(b)] ||
			label(a.job_name).localeCompare(label(b.job_name)),
	);
	const passed = sorted.filter((j) => jobStatus(j) === "success");
	const pill = (job: CIJob) => {
		const s = jobStatus(job);
		return (
			<Pill
				key={job.job_name}
				status={s === "none" ? undefined : s}
				href={sippyJobUrl(version, job.job_name)}
				tip={
					<>
						{equivalent && (
							<div style={muted}>
								Ran on another FBC with the same bundle; its channel graph can
								differ.
							</div>
						)}
						<div>{job.job_name}</div>
						{job.runs.map((r) => (
							<div key={r.prow_url}>
								<ExtLink href={r.prow_url}>{when(r.started_at)}</ExtLink>
								{` · ${r.install_failed ? "Install failed" : runState(r.state)}`}
							</div>
						))}
					</>
				}
			>
				{mark}
				{label(job.job_name)}
				{job.runs.length > 1 && ` ×${job.runs.length}`}
			</Pill>
		);
	};
	return (
		<Flex gap={{ default: "gapXs" }}>
			{sorted.filter((j) => jobStatus(j) !== "success").map(pill)}
			{passed.length > 0 && (
				<Label
					isCompact
					isClickable
					status="success"
					render={({ className, content, componentRef }) => (
						<button
							type="button"
							className={className}
							ref={componentRef}
							aria-expanded={showPassed}
							onClick={() => setShowPassed(!showPassed)}
						>
							{content}
						</button>
					)}
				>
					{`${mark}${passed.length} passed`}
				</Label>
			)}
			{showPassed && passed.map(pill)}
		</Flex>
	);
}

/**
 * A compact label with a popover, which stays open under the pointer as tip
 * has links, opening href in a new tab; without href it takes keyboard focus,
 * which shows the popover.
 */
function Pill({
	tip,
	href,
	children,
	...props
}: Pick<LabelProps, "status" | "children"> & {
	tip: ReactNode;
	href?: string | null;
}) {
	const [open, setOpen] = useState<"hover" | "focus">();
	const label = href ? (
		<Label
			isCompact
			isClickable
			render={({ className, content, componentRef }) => (
				<a className={className} href={href} ref={componentRef} {...external}>
					{content}
				</a>
			)}
			{...props}
		>
			{children}
		</Label>
	) : (
		<Label isCompact tabIndex={0} {...props}>
			{children}
		</Label>
	);
	return (
		// Open while focus is inside, and inline then so Tab reaches tip's links;
		// on hover it stays in body, where the card's overflow cannot flip it.
		// biome-ignore lint/a11y/noStaticElementInteractions: tracks focus, not input
		<span
			style={{ display: "contents" }}
			onFocus={(e) => {
				// React bubbles focus out of the hover portal; going inline then
				// would remount the link under a pending click.
				if (e.currentTarget.contains(e.target)) setOpen("focus");
			}}
			onBlur={(e) => {
				if (!e.currentTarget.contains(e.relatedTarget)) setOpen(undefined);
			}}
		>
			<Popover
				triggerAction="hover"
				isVisible={!!open}
				shouldOpen={() => setOpen((o) => o ?? "hover")}
				shouldClose={() => setOpen(undefined)}
				appendTo={open === "focus" ? "inline" : undefined}
				aria-label="Runs"
				bodyContent={tip}
			>
				{label}
			</Popover>
		</span>
	);
}

function TicketsCard({
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
	const hasIssues = (issues ?? []).length > 0;
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
				{hasIssues && ` (${issues!.length})`}
			</LinkedCardTitle>
			<CardBody>
				{hasIssues ? (
					<IssuesTable issues={issues!} />
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

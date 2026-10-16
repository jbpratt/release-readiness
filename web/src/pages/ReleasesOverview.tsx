import {
	Button,
	EmptyState,
	EmptyStateActions,
	EmptyStateBody,
	EmptyStateFooter,
	Flex,
	Icon,
	PageSection,
	Spinner,
	Switch,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	CubesIcon,
	ExclamationTriangleIcon,
	InfoCircleIcon,
	TimesCircleIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type { ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { getCandidate, listReleasesOverview } from "../api/client";
import type { ReleaseCandidate, ReleaseOverview } from "../api/types";
import LoadError, { Loading } from "../components/LoadError";
import OpenIn from "../components/OpenIn";
import { ExtLink, external, useNarrow } from "../components/OperatorImages";
import { ShipmentLink } from "../components/ShipmentLabels";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { blockers } from "../utils/blockers";
import {
	candidateSummary,
	cellProblem,
	streamFbc,
	type Tone,
} from "../utils/fbcMatrix";
import { days, due } from "../utils/format";
import {
	ART_PIPELINES_HEALTH_URL,
	formatReleaseName,
	jiraIssueUrl,
} from "../utils/links";
import { fbcGroups, verdictEntries } from "../utils/releaseDetail";

const muted = { color: "var(--pf-t--global--text--color--subtle)" };
const dash = <span style={muted}>–</span>;

const releasePath = (name: string) => `/releases/${encodeURIComponent(name)}`;

export default function ReleasesOverview() {
	const [searchParams, setSearchParams] = useSearchParams();
	const showAll = searchParams.get("all") === "1";
	const setShowAll = (on: boolean) =>
		setSearchParams((prev) => {
			const next = new URLSearchParams(prev);
			if (on) next.set("all", "1");
			else next.delete("all");
			return next;
		});

	const {
		data: overviews,
		error,
		refetch,
	} = useCachedFetch("releasesOverview", listReleasesOverview);
	const narrow = useNarrow();

	if (!overviews && !error) {
		return (
			<PageSection>
				<Loading title="Loading releases…" />
			</PageSection>
		);
	}

	if (!overviews) {
		return (
			<PageSection>
				<LoadError title="Could not load releases" onRetry={refetch}>
					The server did not return the release list. Try again in a moment.
				</LoadError>
			</PageSection>
		);
	}

	const overviewList = [...overviews].sort((a, b) =>
		b.release.name.localeCompare(a.release.name, undefined, { numeric: true }),
	);
	// Only the next z-release of each stream, unless every version is asked for.
	const visible = showAll
		? overviewList
		: overviewList.filter((ov) => ov.next_in_stream);

	if (overviewList.length === 0) {
		return (
			<PageSection>
				<EmptyState
					icon={CubesIcon}
					titleText="No releases yet"
					headingLevel="h2"
				>
					<EmptyStateBody>
						Releases appear once the Jira sync finds them.
					</EmptyStateBody>
					<EmptyStateFooter>
						<EmptyStateActions>
							<a href="/api/v1/sync-status" {...external}>
								Sync status
							</a>
						</EmptyStateActions>
					</EmptyStateFooter>
				</EmptyState>
			</PageSection>
		);
	}

	return (
		<PageSection>
			<Flex
				justifyContent={{ default: "justifyContentSpaceBetween" }}
				alignItems={{ default: "alignItemsFlexEnd" }}
				style={{ marginBottom: "1rem" }}
			>
				<Title headingLevel="h1" size="2xl">
					Releases
				</Title>
				<Flex
					gap={{ default: "gapLg" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<Switch
						id="show-all-versions"
						label="Show all versions"
						isChecked={showAll}
						onChange={(_e, on) => setShowAll(on)}
					/>
					<OpenIn
						items={[{ text: "ART pipelines", href: ART_PIPELINES_HEALTH_URL }]}
					/>
				</Flex>
			</Flex>

			{visible.length === 0 ? (
				<EmptyState
					icon={CubesIcon}
					titleText="No next version in any stream"
					headingLevel="h2"
				>
					<EmptyStateFooter>
						<EmptyStateActions>
							<Button variant="primary" onClick={() => setShowAll(true)}>
								Show all versions
							</Button>
						</EmptyStateActions>
					</EmptyStateFooter>
				</EmptyState>
			) : (
				<Table aria-label="Releases" variant="compact" gridBreakPoint="">
					<Thead>
						{narrow ? (
							<Tr>
								<Th>Release</Th>
							</Tr>
						) : (
							<Tr>
								<Th>Release</Th>
								<Th>Target</Th>
								<Th>Release candidate</Th>
								<Th>Shipment</Th>
								<Th>Ticket</Th>
								<Th>Assignee</Th>
							</Tr>
						)}
					</Thead>
					<Tbody>
						{visible.map((ov) => (
							<ReleaseRow key={ov.release.name} ov={ov} narrow={narrow} />
						))}
					</Tbody>
				</Table>
			)}
		</PageSection>
	);
}

/**
 * One release's facts in a row, its candidate read from the release page's
 * API. Narrow, the row is two lines: version and candidate, then the rest.
 */
function ReleaseRow({ ov, narrow }: { ov: ReleaseOverview; narrow: boolean }) {
	const config = useConfig();
	const r = ov.release;
	const candidate = useCachedFetch(`candidate:${r.name}`, () =>
		getCandidate(r.name),
	);
	const shipped = candidate.data?.shipped ?? r.released;
	const target = r.due_date ?? r.release_date;
	const dueDate = target ? due(target, shipped) : undefined;
	const ticket = r.release_ticket_key;
	const verdict = candidate.data?.verdict;
	const blockerCount =
		verdict?.state === "not_ready"
			? verdictEntries(verdict)
					.filter((e) => e.level === "blocker")
					.reduce((n, e) => n + e.count, 0)
			: 0;
	const flag =
		dueDate?.kind === "past"
			? "Past due"
			: dueDate?.kind === "soon"
				? dueDate.days === 0
					? "Due today"
					: `Due in ${days(dueDate.days)}`
				: null;
	const name = (
		<>
			<Link to={releasePath(r.name)} style={{ fontWeight: 600 }}>
				{formatReleaseName(r.name)}
			</Link>
			{/* What blocks it, for screen readers only: no visible verdict. */}
			<span role="status" className="pf-v6-screen-reader">
				{blockerCount > 0 &&
					`${formatReleaseName(r.name)}: ${blockerCount} blocker${blockerCount === 1 ? "" : "s"}`}
			</span>
		</>
	);
	const targetDate = (
		<span style={{ whiteSpace: "nowrap" }}>
			{dueDate?.date ?? <span style={muted}>TBD</span>}
			{flag && <Flag text={flag} />}
		</span>
	);
	const candidateCell = (
		<CandidateCell ov={ov} data={candidate.data} error={candidate.error} />
	);
	const shipment = ov.shipment_mr && (
		<ShipmentLink mr={ov.shipment_mr} shipped={shipped} />
	);
	const ticketLink = ticket && (
		<ExtLink
			href={jiraIssueUrl(
				ticket,
				config?.jira_base_url || "https://redhat.atlassian.net",
			)}
			icon
		>
			{ticket}
		</ExtLink>
	);
	const assignee = r.release_ticket_assignee || (
		<span style={muted}>Unassigned</span>
	);
	if (narrow)
		return (
			<Tr>
				<Td dataLabel="Release">
					<Flex
						columnGap={{ default: "columnGapMd" }}
						flexWrap={{ default: "nowrap" }}
						alignItems={{ default: "alignItemsBaseline" }}
					>
						<span style={{ whiteSpace: "nowrap" }}>{name}</span>
						<span style={{ minWidth: 0 }}>{candidateCell}</span>
					</Flex>
					<Flex
						columnGap={{ default: "columnGapMd" }}
						rowGap={{ default: "rowGapXs" }}
						alignItems={{ default: "alignItemsCenter" }}
						style={{
							fontSize: "var(--pf-t--global--font--size--sm)",
							marginTop: "0.25rem",
						}}
					>
						{targetDate}
						<span style={{ whiteSpace: "nowrap" }}>
							{ticketLink || <span style={muted}>No ticket</span>}
						</span>
						<span style={{ whiteSpace: "nowrap" }}>{assignee}</span>
						{shipment}
					</Flex>
				</Td>
			</Tr>
		);
	return (
		<Tr>
			<Td dataLabel="Release">{name}</Td>
			<Td dataLabel="Target">{targetDate}</Td>
			<Td dataLabel="Release candidate">{candidateCell}</Td>
			<Td dataLabel="Shipment">{shipment || dash}</Td>
			<Td dataLabel="Ticket">{ticketLink || dash}</Td>
			<Td dataLabel="Assignee">{assignee}</Td>
		</Tr>
	);
}

/** A due flag as a warning icon and its words, no filled label. */
function Flag({ text }: { text: string }) {
	return (
		<span
			style={{
				marginLeft: "0.5rem",
				display: "inline-flex",
				gap: "0.25rem",
				alignItems: "center",
			}}
		>
			<Icon status="warning" isInline>
				<ExclamationTriangleIcon />
			</Icon>
			{text}
		</span>
	);
}

const toneIcons: Record<Tone, ReactNode> = {
	danger: (
		<Icon status="danger">
			<TimesCircleIcon />
		</Icon>
	),
	warning: (
		<Icon status="warning">
			<ExclamationTriangleIcon />
		</Icon>
	),
	info: (
		<Icon status="info">
			<InfoCircleIcon />
		</Icon>
	),
	running: <Icon isInProgress />,
	success: (
		<Icon status="success">
			<CheckCircleIcon />
		</Icon>
	),
};

// The icon's tone in words: the link's text is all a screen reader gets.
const severityWords: Partial<Record<Tone, string>> = {
	danger: "Blocked: ",
	warning: "Warning: ",
};

/**
 * The candidate as one icon and a few words linking the release page: its
 * first blocker or warning and who owns it, else how many of its FBCs are green.
 */
function CandidateCell({
	ov,
	data,
	error,
}: {
	ov: ReleaseOverview;
	data?: ReleaseCandidate;
	error?: Error;
}) {
	const config = useConfig();
	if (!data)
		return error ? (
			<span style={muted}>Could not load</span>
		) : (
			<Spinner size="sm" aria-label="Loading the release candidate" />
		);
	const fbc = data.fbc_candidate;
	if (!fbc) {
		const [label, tip] =
			data.reason === "not a concrete quay-vX.Y.Z version"
				? ["Not expected", "Only Quay versions have FBC builds"]
				: data.shipped
					? ["No FBC data", "No FBC data: this version has shipped"]
					: ["No FBCs yet", "No FBCs yet: ART has not built one"];
		return (
			<Tooltip content={tip}>
				{/* biome-ignore lint/a11y/noNoninteractiveTabindex: focus shows its tooltip. */}
				<span tabIndex={0} style={muted}>
					<span aria-hidden="true">–</span>
					<span className="pf-v6-screen-reader">{label}</span>
				</span>
			</Tooltip>
		);
	}
	const staged = fbc.source !== "stream";
	const cells = fbcGroups(data).flatMap((g) =>
		g.rows.map((row) => {
			const stream = row.fbc ? null : streamFbc(data.fbc_stream, row);
			return { row, stream, problem: cellProblem(row, staged, stream) };
		}),
	);
	const bs = blockers(
		data,
		ov.release.name,
		ov.release.konflux_application ?? "",
		config,
	);
	const s = candidateSummary(cells, bs);
	// Who acts on the gap the words name: the first blocker's, else warning's, owner.
	const owner = (
		bs.find((b) => b.level === "blocker") ??
		bs.find((b) => b.level === "warning")
	)?.owner;
	return (
		<span
			style={{
				display: "inline-flex",
				flexWrap: "wrap",
				columnGap: "0.5rem",
				alignItems: "center",
			}}
		>
			{toneIcons[s.tone]}
			<Tooltip content={s.all}>
				<Link to={releasePath(ov.release.name)}>
					{severityWords[s.tone] && (
						<span className="pf-v6-screen-reader">{severityWords[s.tone]}</span>
					)}
					{s.words}
					{s.more && (
						<span style={{ whiteSpace: "nowrap" }}>{`\u00a0${s.more}`}</span>
					)}
				</Link>
			</Tooltip>
			{owner && (
				<span style={muted}>
					<span aria-hidden="true">· </span>
					<span className="pf-v6-screen-reader">Owner: </span>
					{owner}
				</span>
			)}
		</span>
	);
}

import {
	DrawerActions,
	DrawerCloseButton,
	DrawerHead,
	DrawerPanelBody,
	DrawerPanelContent,
	EmptyState,
	EmptyStateBody,
	Flex,
	Icon,
	Label,
	Spinner,
	Title,
	Tooltip,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	CubesIcon,
	TimesCircleIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type { FBCBuild, FBCRun } from "../api/types";
import LoadError from "../components/LoadError";
import { ExtLink, muted } from "../components/OperatorImages";
import { Status } from "../components/Status";
import {
	bundleBuilt,
	ciBuilds,
	jobShortName,
	runResult,
} from "../utils/releaseDetail";

/** A time as the cell popover's Built line has it: "Oct 9 04:45 UTC". */
const utc = (iso: string) =>
	`${new Date(iso)
		.toLocaleString("en-US", {
			timeZone: "UTC",
			month: "short",
			day: "numeric",
			hour: "2-digit",
			minute: "2-digit",
			hourCycle: "h23",
		})
		.replace(",", "")} UTC`;

const states = {
	candidate: ["Candidate", "blue"],
	staged: ["Staged", "teal"],
	shipped: ["Shipped", "green"],
} as const;

/** A periodic job's install suite: "aws-s3", "aws-s3 fips". */
const suite = (job: string) => jobShortName(job).replace(/-fips$/, " fips");

/** The matrix's red X. */
const failedIcon = (
	<Icon status="danger" isInline>
		<TimesCircleIcon />
	</Icon>
);

function RunState({ state }: { state: string }) {
	const { status, text } = runResult(state);
	if (status === "failed")
		return (
			<Flex gap={{ default: "gapSm" }} flexWrap={{ default: "nowrap" }}>
				{failedIcon}
				<span>{text}</span>
			</Flex>
		);
	return status ? <Status status={status}>{text}</Status> : text;
}

/** Passed and failed counts as icon and numeral; the words in the name. */
function Tally({ runs }: { runs: FBCRun[] }) {
	const count = (s: string) =>
		runs.filter((r) => runResult(r.state).status === s).length;
	const pass = count("done");
	const fail = count("failed");
	const words = `${pass} passed, ${fail} failed`;
	return (
		<span
			role="img"
			aria-label={words}
			title={words}
			style={{ display: "inline-flex", gap: "0.75rem", whiteSpace: "nowrap" }}
		>
			{pass > 0 && (
				<span>
					<Icon status="success" isInline>
						<CheckCircleIcon />
					</Icon>{" "}
					{pass}
				</span>
			)}
			{fail > 0 && (
				<span>
					{failedIcon} {fail}
				</span>
			)}
		</span>
	);
}

/** The build's operator and OCP (with every build), state, and build time, the NVR on hover. */
function BuildHead({ b, all }: { b: FBCBuild; all: boolean }) {
	const nvrTime = bundleBuilt(b.nvr);
	const built = b.built_at
		? utc(b.built_at)
		: nvrTime
			? `${nvrTime} UTC`
			: "unknown";
	return (
		<Flex
			gap={{ default: "gapSm" }}
			alignItems={{ default: "alignItemsCenter" }}
		>
			{all && (
				<span>
					{b.operator} · OCP {b.ocp}
				</span>
			)}
			{b.state && (
				<Label isCompact color={states[b.state][1]}>
					{states[b.state][0]}
				</Label>
			)}
			<Tooltip content={b.nvr}>
				{/* biome-ignore lint/a11y/noNoninteractiveTabindex: focus shows the NVR. */}
				<span tabIndex={0} style={{ ...muted, fontWeight: "normal" }}>
					Built {built}
				</span>
			</Tooltip>
		</Flex>
	);
}

/**
 * The release page's CI drawer: one cell's FBC builds (cell
 * "<operator>@<ocp>"), the candidate's first, or every build with a run
 * (cell "all"); each build a header row over the CI runs that installed from
 * it, each run's start linking to Prow. Closed while cell is null.
 */
export default function CIDrawer({
	cell,
	builds,
	error,
	onRetry,
	onClose,
}: {
	cell: string | null;
	builds?: FBCBuild[];
	error?: Error;
	onRetry: () => void;
	onClose: () => void;
}) {
	const all = cell === "all";
	const [operator, ocp] = (cell ?? "").split("@");
	const shown = ciBuilds(builds ?? [], cell ?? "");
	return (
		<DrawerPanelContent
			widths={{ default: "width_100", lg: "width_50", xl: "width_33" }}
			// PF marks the closed panel inert="", which React 19 warns about.
			inert={!cell}
		>
			<DrawerHead>
				<Title headingLevel="h2" size="lg">
					{all ? "CI history" : `${operator} · OCP ${ocp}`}
				</Title>
				<span style={muted}>
					{all ? "Every FBC build with a CI run" : "CI runs by FBC build"}
				</span>
				<DrawerActions>
					<DrawerCloseButton onClick={onClose} />
				</DrawerActions>
			</DrawerHead>
			<DrawerPanelBody>
				{!builds && !error ? (
					<Spinner size="md" aria-label="Loading CI runs" />
				) : !builds ? (
					<LoadError
						variant="xs"
						title="Could not load CI runs"
						onRetry={onRetry}
					>
						The server did not return this release's FBC builds and their CI
						runs. Try again in a moment.
					</LoadError>
				) : !shown.some((b) => b.runs.length > 0) ? (
					<EmptyState
						variant="xs"
						headingLevel="h3"
						icon={CubesIcon}
						titleText="No CI runs"
					>
						<EmptyStateBody>
							{builds.length === 0
								? "CI runs show here once ART builds an FBC for this version."
								: all
									? "No CI run has installed one of this version's FBCs yet."
									: `No CI run has installed a ${operator} FBC for OCP ${ocp} yet.`}
						</EmptyStateBody>
					</EmptyState>
				) : (
					<Table
						variant="compact"
						aria-label="CI runs by FBC build"
						gridBreakPoint=""
					>
						<Thead>
							<Tr>
								<Th modifier="nowrap">Suite</Th>
								<Th modifier="nowrap">Result</Th>
								<Th modifier="nowrap">Started</Th>
							</Tr>
						</Thead>
						{shown.map((b) => (
							<Tbody key={b.nvr}>
								<Tr>
									<Th
										colSpan={3}
										scope="colgroup"
										style={{
											background:
												"var(--pf-t--global--background--color--secondary--default)",
										}}
									>
										<Flex
											justifyContent={{
												default: "justifyContentSpaceBetween",
											}}
											alignItems={{ default: "alignItemsCenter" }}
										>
											<BuildHead b={b} all={all} />
											{b.runs.length > 0 ? (
												<Tally runs={b.runs} />
											) : (
												<span style={{ ...muted, fontWeight: "normal" }}>
													No runs
												</span>
											)}
										</Flex>
									</Th>
								</Tr>
								{b.runs.map((r) => (
									<Tr key={r.prow_url}>
										<Td dataLabel="Suite" modifier="nowrap">
											<Tooltip content={r.job}>
												{/* biome-ignore lint/a11y/noNoninteractiveTabindex: focus shows the job name. */}
												<span tabIndex={0}>{suite(r.job)}</span>
											</Tooltip>
										</Td>
										<Td dataLabel="Result" modifier="nowrap">
											<RunState state={r.state} />
										</Td>
										<Td dataLabel="Started" modifier="nowrap">
											<ExtLink href={r.prow_url} icon>
												{utc(r.started_at)}
											</ExtLink>
										</Td>
									</Tr>
								))}
							</Tbody>
						))}
					</Table>
				)}
			</DrawerPanelBody>
		</DrawerPanelContent>
	);
}

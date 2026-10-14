import {
	Breadcrumb,
	BreadcrumbHeading,
	BreadcrumbItem,
	Checkbox,
	EmptyState,
	EmptyStateBody,
	Flex,
	FormSelect,
	FormSelectOption,
	Label,
	PageSection,
	Spinner,
	Title,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
	Tooltip,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { getReleaseCI } from "../api/client";
import type { FBCBuild, FBCRun } from "../api/types";
import LoadError from "../components/LoadError";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { formatReleaseName } from "../utils/links";
import { compareVersions, jobLabels } from "../utils/releaseDetail";

const external = { target: "_blank", rel: "noopener noreferrer" };
const muted = { color: "var(--pf-t--global--text--color--subtle)" };

const when = (iso: string) =>
	new Date(iso).toLocaleString(undefined, {
		month: "short",
		day: "numeric",
		hour: "2-digit",
		minute: "2-digit",
		hourCycle: "h23",
	});

const states = {
	candidate: ["Candidate", "blue"],
	staged: ["Staged", "teal"],
	shipped: ["Shipped", "green"],
} as const;

/**
 * The route version's FBC builds, the operator, OCP and all-builds filters
 * kept in query parameters, and the builds those filters show.
 */
function useReleaseCI() {
	const { version } = useParams<{ version: string }>();
	const [params, setParams] = useSearchParams();
	const { data, loading, error, refetch } = useCachedFetch(
		`ci:${version}`,
		() => getReleaseCI(version!),
	);
	const operator = params.get("operator") ?? "";
	const ocp = params.get("ocp") ?? "";
	const all = params.get("all") === "1";
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

	const builds = data ?? [];
	const operators = [...new Set(builds.map((b) => b.operator))].sort();
	const ocps = [...new Set(builds.map((b) => b.ocp))].sort(compareVersions);
	const jobs = [...new Set(builds.flatMap((b) => b.runs.map((r) => r.job)))];
	const labels = jobLabels(jobs);
	const matching = builds.filter(
		(b) => (!operator || b.operator === operator) && (!ocp || b.ocp === ocp),
	);
	const shown = matching.filter((b) => all || b.runs.length > 0);
	return {
		version: version!,
		data,
		loading,
		error,
		refetch,
		operator,
		ocp,
		all,
		set,
		operators,
		ocps,
		label: (job: string) => labels[jobs.indexOf(job)],
		matching: matching.length,
		shown,
	};
}

function CIBreadcrumb({ version }: { version: string }) {
	return (
		<Breadcrumb style={{ marginBottom: "1rem" }}>
			<BreadcrumbItem>
				<Link to="/">Releases</Link>
			</BreadcrumbItem>
			<BreadcrumbItem>
				<Link to={`/releases/${version}`}>{formatReleaseName(version)}</Link>
			</BreadcrumbItem>
			<BreadcrumbHeading>FBC CI history</BreadcrumbHeading>
		</Breadcrumb>
	);
}

function CIFilters({ ci }: { ci: ReturnType<typeof useReleaseCI> }) {
	return (
		<Toolbar style={{ marginBottom: 24 }}>
			<ToolbarContent>
				<ToolbarItem>
					<FormSelect
						value={ci.operator}
						onChange={(_e, v) => ci.set("operator", v)}
						aria-label="Operator"
					>
						<FormSelectOption value="" label="All operators" />
						{ci.operators.map((o) => (
							<FormSelectOption key={o} value={o} label={o} />
						))}
					</FormSelect>
				</ToolbarItem>
				<ToolbarItem>
					<FormSelect
						value={ci.ocp}
						onChange={(_e, v) => ci.set("ocp", v)}
						aria-label="OCP version"
						style={{ width: "12rem" }}
					>
						<FormSelectOption value="" label="All OCP versions" />
						{ci.ocps.map((o) => (
							<FormSelectOption key={o} value={o} label={`OCP ${o}`} />
						))}
					</FormSelect>
				</ToolbarItem>
				<ToolbarItem alignSelf="center">
					<Checkbox
						id="all"
						label="Include builds with no runs"
						isChecked={ci.all}
						onChange={(_e, v) => ci.set("all", v ? "1" : "")}
					/>
				</ToolbarItem>
			</ToolbarContent>
		</Toolbar>
	);
}

const runStates: Record<string, [string, "green" | "red" | "blue" | "grey"]> = {
	success: ["Passed", "green"],
	failure: ["Failed", "red"],
	error: ["Error", "red"],
	pending: ["Running", "blue"],
	aborted: ["Aborted", "grey"],
};

function RunState({ state }: { state: string }) {
	const [text, color] = runStates[state] ?? [state, "grey"];
	return (
		<Label isCompact color={color}>
			{text}
		</Label>
	);
}

/**
 * When the FBC was built, from built_at or else the NVR's UTC timestamp, with
 * the full NVR on hover: the time is what tells two builds apart.
 */
function BuildTime({ build }: { build: FBCBuild }) {
	const ts = build.nvr.match(/-(\d{4})(\d\d)(\d\d)(\d\d)(\d\d)\d\d\./);
	const at =
		build.built_at ??
		(ts && `${ts[1]}-${ts[2]}-${ts[3]}T${ts[4]}:${ts[5]}:00Z`);
	return (
		<Tooltip content={build.nvr}>
			<span style={{ ...muted, textDecoration: "underline dotted" }}>
				Built {at ? when(at) : "unknown"}
			</span>
		</Tooltip>
	);
}

function BuildTitle({ build }: { build: FBCBuild }) {
	return (
		<Flex
			gap={{ default: "gapSm" }}
			alignItems={{ default: "alignItemsCenter" }}
		>
			<span>
				{build.operator} · OCP {build.ocp}
			</span>
			{build.state && (
				<Label isCompact color={states[build.state][1]}>
					{states[build.state][0]}
				</Label>
			)}
		</Flex>
	);
}

/**
 * A version's FBC builds, newest first, one table row per CI run that
 * installed from a build (or per build without runs), each run's start time
 * linking to Prow. The operator and OCP filters and whether builds without
 * runs show are query parameters.
 */
export default function ReleaseCI() {
	const ci = useReleaseCI();
	const { version, data, loading, error, refetch, shown } = ci;
	const rows = shown.flatMap((b) =>
		b.runs.length > 0
			? b.runs.map((r): { b: FBCBuild; r?: FBCRun } => ({ b, r }))
			: [{ b }],
	);

	return (
		<PageSection>
			<CIBreadcrumb version={version} />
			<Title headingLevel="h1" size="xl" style={{ marginBottom: 8 }}>
				FBC CI history
			</Title>
			<CIFilters ci={ci} />
			{!data && loading ? (
				<Spinner size="lg" />
			) : error ? (
				<LoadError
					variant="sm"
					title="Could not load CI history"
					onRetry={refetch}
				>
					The server did not return this release's FBC builds and their CI runs.
					Try again in a moment.
				</LoadError>
			) : !data ? (
				<EmptyState
					variant="sm"
					icon={Spinner}
					titleText="Loading CI history…"
				/>
			) : shown.length === 0 ? (
				<EmptyState variant="sm" titleText="No FBC builds to show">
					<EmptyStateBody>
						{data.length === 0
							? "This version has no FBC builds."
							: ci.matching === 0
								? "No build matches these filters."
								: `None of ${ci.operator || ci.ocp ? "the matching" : "this version's"} FBC builds has a CI run yet. Include builds with no runs to list them.`}
					</EmptyStateBody>
				</EmptyState>
			) : (
				<Table variant="compact" aria-label="FBC CI runs">
					<Thead>
						<Tr>
							<Th>Build</Th>
							<Th>Job</Th>
							<Th>Result</Th>
							<Th>Started</Th>
						</Tr>
					</Thead>
					<Tbody>
						{rows.map(({ b, r }) => (
							<Tr key={r?.prow_url ?? b.nvr}>
								<Td dataLabel="Build">
									<Flex
										direction={{ default: "column" }}
										gap={{ default: "gapXs" }}
									>
										<BuildTitle build={b} />
										<BuildTime build={b} />
									</Flex>
								</Td>
								<Td dataLabel="Job">
									{r ? (
										<Tooltip content={r.job}>
											<span style={{ overflowWrap: "anywhere" }}>
												{ci.label(r.job)}
											</span>
										</Tooltip>
									) : (
										<span style={muted}>No runs</span>
									)}
								</Td>
								<Td dataLabel="Result">
									{/* A div, so the stacked layout does not stretch the Label. */}
									<div>{r && <RunState state={r.state} />}</div>
								</Td>
								<Td dataLabel="Started">
									{r && (
										<a href={r.prow_url} {...external}>
											{when(r.started_at)}
										</a>
									)}
								</Td>
							</Tr>
						))}
					</Tbody>
				</Table>
			)}
		</PageSection>
	);
}

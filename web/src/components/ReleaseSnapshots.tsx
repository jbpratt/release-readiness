import {
	Alert,
	Button,
	Card,
	CardBody,
	CardFooter,
	CardHeader,
	CardTitle,
	Content,
	EmptyState,
	EmptyStateBody,
	Flex,
	FlexItem,
	HelperText,
	HelperTextItem,
	Label,
	Pagination,
	Spinner,
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
	Tooltip,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
	getReleaseSnapshot,
	getSnapshotProwRuns,
	listReleaseSnapshots,
} from "../api/client";
import type {
	KonfluxRelease,
	ReleaseSnapshot,
	ReleaseVersion,
	SnapshotImage,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { relative } from "../utils/format";
import {
	latestReleased,
	type SnapshotFilter,
	snapshotFilters,
} from "../utils/releaseApps";
import ReleasedLabel from "./ReleasedLabel";
import SnapshotComponentsTable from "./SnapshotComponentsTable";

const PAGE_SIZE = 25;

const quayFilter = (filters: SnapshotFilter[]) =>
	filters.find((f) => f.key === "quay") ?? filters[0];

/**
 * A release's latest Quay snapshot and its images, fetched once for both the
 * release status stepper and the latest snapshot card.
 */
export function useLatestSnapshot(version: string, release?: ReleaseVersion) {
	const { application } = quayFilter(
		snapshotFilters(release?.konflux_application),
	);
	const list = useCachedFetch(
		release ? `latestSnapshot:${version}:${application}` : null,
		() => listReleaseSnapshots(version, { application, limit: 1, offset: 0 }),
	);
	const snapshot = list.data?.snapshots[0];
	const detail = useCachedFetch(
		snapshot && !snapshot.missing
			? `releaseSnapshot:${version}:${snapshot.name}`
			: null,
		() => getReleaseSnapshot(version, snapshot!.name),
	);
	return {
		application,
		snapshot,
		// Also covers the render before the fetch starts, once release loads.
		loading: !list.data && !list.error,
		error: list.error,
		detail,
	};
}

type LatestSnapshotState = ReturnType<typeof useLatestSnapshot>;

/** A release's latest unreleased or released Quay snapshot. */
export function LatestSnapshot({
	version,
	state,
}: {
	version: string;
	state: LatestSnapshotState;
}) {
	const { application, snapshot: latest } = state;
	const [view, setView] = useState<"unreleased" | "released">("unreleased");
	const releasedList = useCachedFetch(
		`releasedSnapshots:${version}:${application}`,
		() =>
			listReleaseSnapshots(version, {
				application,
				withRelease: true,
				limit: 100,
				offset: 0,
			}),
	);
	const released =
		releasedList.data && latestReleased(releasedList.data.snapshots);
	// Unreleased is the newest Snapshot when it postdates the released one.
	const unreleased =
		latest && (!released || latest.created_at > released.created_at)
			? latest
			: undefined;
	const releasedDetail = useCachedFetch(
		unreleased && released && !released.missing
			? `releaseSnapshot:${version}:${released.name}`
			: null,
		() => getReleaseSnapshot(version, released!.name),
	);
	const changed = changedSince(
		state.detail.data?.components,
		releasedDetail.data?.components,
	);
	const loading = state.loading || (!releasedList.data && !releasedList.error);
	const error = state.error ?? releasedList.error;

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardHeader
				actions={{
					hasNoOffset: true,
					actions: (
						<ToggleGroup aria-label="Snapshot" isCompact>
							<ToggleGroupItem
								text="Latest unreleased"
								isSelected={view === "unreleased"}
								onChange={() => setView("unreleased")}
							/>
							<ToggleGroupItem
								text="Latest released"
								isSelected={view === "released"}
								onChange={() => setView("released")}
							/>
						</ToggleGroup>
					),
				}}
			>
				<CardTitle>Latest snapshot</CardTitle>
			</CardHeader>
			<CardBody>
				{loading ? (
					<Spinner size="md" />
				) : error ? (
					<Content component="p">{error.message}</Content>
				) : view === "released" ? (
					released ? (
						<SnapshotSummary version={version} snapshot={released} />
					) : (
						<EmptyState
							titleText="No released snapshot"
							headingLevel="h4"
							variant="xs"
						>
							<EmptyStateBody>
								No Konflux Release of {application || "this release"} has
								succeeded yet.
							</EmptyStateBody>
						</EmptyState>
					)
				) : !latest ? (
					<Content component="p">
						No snapshots of {application || "this release"} yet.
					</Content>
				) : !unreleased ? (
					<EmptyState
						titleText="Latest snapshot is already released"
						headingLevel="h4"
						variant="xs"
					/>
				) : (
					<>
						{released && changed && (
							<Alert
								variant="info"
								isInline
								isPlain
								title={`Last released: ${released.name}, ${changed.size} of ${state.detail.data?.components?.length} images differ`}
								style={{ marginBottom: "0.5rem" }}
							/>
						)}
						<SnapshotSummary
							version={version}
							snapshot={unreleased}
							fetched={state.detail}
							changed={changed}
						/>
					</>
				)}
			</CardBody>
			<CardFooter>
				<Button
					variant="link"
					isInline
					component={(props: object) => (
						<Link
							{...props}
							to={`/releases/${encodeURIComponent(version)}/snapshots`}
						/>
					)}
				>
					View snapshot history
				</Button>
			</CardFooter>
		</Card>
	);
}

/** Names of the components whose image is not the released one. */
function changedSince(current?: SnapshotImage[], released?: SnapshotImage[]) {
	if (!current || !released) return undefined;
	const shipped = new Map(released.map((c) => [c.name, c.image]));
	return new Set(
		current.filter((c) => shipped.get(c.name) !== c.image).map((c) => c.name),
	);
}

function SnapshotSummary({
	version,
	snapshot,
	fetched,
	changed,
}: {
	version: string;
	snapshot: ReleaseSnapshot;
	fetched?: LatestSnapshotState["detail"];
	changed?: Set<string>;
}) {
	return (
		<>
			<Flex
				alignItems={{ default: "alignItemsCenter" }}
				style={{ marginBottom: "0.5rem" }}
			>
				<FlexItem>
					<code>{snapshot.name}</code>
				</FlexItem>
				<FlexItem>
					<CreatedAt iso={snapshot.created_at} />
				</FlexItem>
				<FlexItem>
					<SnapshotReleaseLabel releases={snapshot.releases} />
				</FlexItem>
			</Flex>
			<SnapshotDetail
				version={version}
				snapshot={snapshot}
				fetched={fetched}
				changed={changed}
			/>
		</>
	);
}

/** Newest-first, paginated Snapshots of a release's applications. */
export function SnapshotHistory({
	version,
	konfluxApp,
}: {
	version: string;
	konfluxApp?: string;
}) {
	const filters = snapshotFilters(konfluxApp);
	const defaultFilter = quayFilter(filters);
	const [searchParams, setSearchParams] = useSearchParams();
	const filter =
		filters.find((f) => f.key === searchParams.get("app")) ?? defaultFilter;
	const withRelease = searchParams.get("with_release") === "true";
	const [page, setPage] = useState(1);
	const [expanded, setExpanded] = useState<string | null>(null);

	const { data, loading, error } = useCachedFetch(
		`releaseSnapshots:${version}:${filter.application}:${withRelease}:${page}`,
		() =>
			listReleaseSnapshots(version, {
				application: filter.application,
				withRelease,
				limit: PAGE_SIZE,
				offset: (page - 1) * PAGE_SIZE,
			}),
	);
	const snapshots = data?.snapshots ?? [];

	const setParam = (key: string, value: string | null) => {
		const next = new URLSearchParams(searchParams);
		if (value) next.set(key, value);
		else next.delete(key);
		setSearchParams(next, { replace: true });
		setPage(1);
	};

	const toggle = (name: string) =>
		setExpanded((prev) => (prev === name ? null : name));

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Snapshot history</CardTitle>
			<CardBody>
				<Toolbar>
					<ToolbarContent>
						{filters.length > 1 && (
							<ToolbarItem>
								<ToggleGroup aria-label="Application">
									{filters.map((f) => (
										<ToggleGroupItem
											key={f.key}
											text={f.label}
											isSelected={f.key === filter.key}
											onChange={() =>
												setParam(
													"app",
													f.key === defaultFilter.key ? null : f.key,
												)
											}
										/>
									))}
								</ToggleGroup>
							</ToolbarItem>
						)}
						<ToolbarItem>
							<ToggleGroup aria-label="Release">
								<ToggleGroupItem
									text="All"
									isSelected={!withRelease}
									onChange={() => setParam("with_release", null)}
								/>
								<ToggleGroupItem
									text="With Release"
									isSelected={withRelease}
									onChange={() => setParam("with_release", "true")}
								/>
							</ToggleGroup>
						</ToolbarItem>
					</ToolbarContent>
				</Toolbar>
				<HelperText style={{ marginBottom: "0.5rem" }}>
					<HelperTextItem>
						Quay images: the release's own components. FBC: its file-based
						catalog (operator index) builds. Base: shared base images. With
						Release: only snapshots a Konflux Release has used.
					</HelperTextItem>
				</HelperText>

				{loading ? (
					<div style={{ textAlign: "center" }}>
						<Spinner />
					</div>
				) : error ? (
					<EmptyState titleText="Error loading snapshots" headingLevel="h3">
						<EmptyStateBody>{error.message}</EmptyStateBody>
					</EmptyState>
				) : snapshots.length === 0 ? (
					<EmptyState titleText="No snapshots" headingLevel="h3">
						<EmptyStateBody>No Snapshots match these filters.</EmptyStateBody>
					</EmptyState>
				) : (
					<>
						<Table variant="compact" aria-label="Snapshot history">
							<Thead>
								<Tr>
									<Th screenReaderText="Row expansion" />
									<Th>Created</Th>
									<Th>Snapshot</Th>
									<Th>Images</Th>
									<Th
										modifier="nowrap"
										info={{
											tooltip:
												"Status of the Konflux Release CR for this snapshot, not whether the product version shipped",
										}}
									>
										Release pipeline
									</Th>
								</Tr>
							</Thead>
							{snapshots.map((s, i) => {
								const isExpanded = expanded === s.name;
								return (
									<Tbody key={s.name} isExpanded={isExpanded}>
										<Tr>
											<Td
												expand={{
													rowIndex: i,
													isExpanded,
													onToggle: () => toggle(s.name),
													expandId: "snapshot-row",
												}}
											/>
											<Td>
												<CreatedAt iso={s.created_at} />
											</Td>
											<Td>
												{s.name}{" "}
												{s.missing && (
													<Label color="orange" isCompact>
														Snapshot not found
													</Label>
												)}
											</Td>
											<Td>{s.missing ? "-" : s.component_count}</Td>
											<Td>
												<SnapshotReleaseLabel releases={s.releases} />
											</Td>
										</Tr>
										<Tr isExpanded={isExpanded}>
											<Td colSpan={5} style={{ padding: 0 }}>
												{isExpanded && (
													<SnapshotDetail version={version} snapshot={s} />
												)}
											</Td>
										</Tr>
									</Tbody>
								);
							})}
						</Table>
						<Pagination
							itemCount={
								data?.has_more
									? page * PAGE_SIZE + 1
									: (page - 1) * PAGE_SIZE + snapshots.length
							}
							perPage={PAGE_SIZE}
							page={page}
							onSetPage={(_, p) => setPage(p)}
							isCompact
							style={{ marginTop: "1rem" }}
						/>
					</>
				)}
			</CardBody>
		</Card>
	);
}

function CreatedAt({ iso }: { iso: string }) {
	return (
		<Tooltip content={new Date(iso).toLocaleString()}>
			<span>{relative(iso)}</span>
		</Tooltip>
	);
}

/** Status of the newest Konflux Release naming the Snapshot; all in the tooltip. */
function SnapshotReleaseLabel({ releases }: { releases?: KonfluxRelease[] }) {
	if (!releases?.length) {
		return (
			<Label color="grey" isCompact>
				Not released
			</Label>
		);
	}
	return (
		<Tooltip
			content={releases.map((r) => (
				<div key={r.name}>
					{r.name}: {r.released_reason || r.released_status || "pending"}
					{r.completion_time &&
						`, completed ${new Date(r.completion_time).toLocaleString()}`}
				</div>
			))}
		>
			<span>
				<ReleasedLabel release={releases[0]} />
				{releases.length > 1 && ` +${releases.length - 1}`}
			</span>
		</Tooltip>
	);
}

function SnapshotDetail({
	version,
	snapshot,
	fetched,
	changed,
}: {
	version: string;
	snapshot: ReleaseSnapshot;
	/** The snapshot's images when the caller already fetches them. */
	fetched?: LatestSnapshotState["detail"];
	changed?: Set<string>;
}) {
	const own = useCachedFetch(
		snapshot.missing || fetched
			? null
			: `releaseSnapshot:${version}:${snapshot.name}`,
		() => getReleaseSnapshot(version, snapshot.name),
	);
	const { data, loading, error } = fetched ?? own;
	const { data: prowRuns } = useCachedFetch(
		snapshot.missing ? null : `snapshotProwRuns:${version}:${snapshot.name}`,
		() => getSnapshotProwRuns(version, snapshot.name),
	);

	if (snapshot.missing || error?.message.startsWith("404")) {
		return (
			<EmptyState titleText="Snapshot not found" headingLevel="h4" variant="xs">
				<EmptyStateBody>
					A Konflux Release names this Snapshot, but it is no longer in the
					cluster, so its images are unknown.
				</EmptyStateBody>
			</EmptyState>
		);
	}
	if (error) return <Content component="p">{error.message}</Content>;
	if (loading || !data) return <Spinner size="md" />;
	return (
		<SnapshotComponentsTable
			components={data.components ?? []}
			prowRuns={prowRuns}
			changed={changed}
		/>
	);
}

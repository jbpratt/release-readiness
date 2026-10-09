import {
	Button,
	Card,
	CardBody,
	CardFooter,
	CardHeader,
	CardTitle,
	Content,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	EmptyState,
	EmptyStateActions,
	EmptyStateBody,
	EmptyStateFooter,
	Flex,
	FlexItem,
	HelperText,
	HelperTextItem,
	Label,
	Pagination,
	Popover,
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
	FBCCatalog,
	KonfluxRelease,
	ReleaseSnapshot,
	ReleaseVersion,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { changedFields } from "../utils/componentDiff";
import { relative } from "../utils/format";
import {
	latestReleased,
	type SnapshotFilter,
	snapshotFilters,
} from "../utils/releaseApps";
import { releaseStatus } from "../utils/releaseStatus";
import SnapshotComponentsTable, {
	type ChangedSince,
	changedDot,
} from "./SnapshotComponentsTable";

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
	const [selected, setView] = useState<"unreleased" | "released">();
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
	const current = state.detail.data?.components;
	const shipped = releasedDetail.data?.components;
	const changed =
		released && current && shipped
			? { since: released.name, fields: changedFields(current, shipped) }
			: undefined;
	const loading = state.loading || (!releasedList.data && !releasedList.error);
	const error = state.error ?? releasedList.error;
	const view = selected ?? (released ? "released" : "unreleased");

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardHeader
				actions={{
					hasNoOffset: true,
					actions: (
						<ToggleGroup aria-label="Snapshot" isCompact>
							<ToggleGroupItem
								text="Latest released"
								isSelected={view === "released"}
								onChange={() => setView("released")}
							/>
							<ToggleGroupItem
								text="Latest unreleased"
								buttonId="latest-unreleased-toggle"
								isSelected={view === "unreleased"}
								onChange={() => setView("unreleased")}
							/>
							{!loading && (
								<Tooltip
									content={unreleasedReason(unreleased, !!released)}
									position="top-end"
									triggerRef={() =>
										document.getElementById("latest-unreleased-toggle")!
									}
								/>
							)}
						</ToggleGroup>
					),
				}}
			>
				<CardTitle>
					Stream snapshot {application && <code>{application}</code>}
				</CardTitle>
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
					released && (
						<EmptyState
							titleText="Nothing pending release"
							headingLevel="h4"
							variant="xs"
							status="info"
						>
							<EmptyStateBody>
								No new build since{" "}
								<Link
									to={`/releases/${encodeURIComponent(version)}/snapshots?with_release=true`}
								>
									<code>{released.name}</code>
								</Link>{" "}
								(built <CreatedAt iso={released.created_at} />) was released.
							</EmptyStateBody>
							<EmptyStateFooter>
								<EmptyStateActions>
									<Button variant="link" onClick={() => setView("released")}>
										Show latest released
									</Button>
								</EmptyStateActions>
							</EmptyStateFooter>
						</EmptyState>
					)
				) : (
					<SnapshotSummary
						version={version}
						snapshot={unreleased}
						fetched={state.detail}
						changed={changed}
						fbc={state.detail.data?.fbc_catalog}
					/>
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

/** Why the newest snapshot has no succeeded Konflux Release. */
function unreleasedReason(snapshot?: ReleaseSnapshot, hasReleased?: boolean) {
	if (!snapshot)
		return hasReleased
			? "No build since the latest released snapshot."
			: "No snapshots of this release yet.";
	const newest = snapshot.releases?.[0];
	if (!newest) return "No Konflux Release names this snapshot yet.";
	return `Its newest Konflux Release: ${releaseStatus(newest).text}.`;
}

function SnapshotSummary({
	version,
	snapshot,
	fetched,
	changed,
	fbc,
}: {
	version: string;
	snapshot: ReleaseSnapshot;
	fetched?: LatestSnapshotState["detail"];
	changed?: ChangedSince;
	fbc?: FBCCatalog;
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
				{fbc && (
					<FlexItem>
						<FBCCatalogLabel fbc={fbc} />
					</FlexItem>
				)}
			</Flex>
			{changed && fetched?.data?.components && (
				<Content component="p" style={{ marginBottom: "0.5rem" }}>
					{changedDot} {changed.fields.size} of {fetched.data.components.length}{" "}
					images changed vs <code>{changed.since}</code>, last released
				</Content>
			)}
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

const fbcLabels = {
	current: { color: "green", text: "FBC current" },
	behind: { color: "orange", text: "FBC behind snapshot" },
	unknown: { color: "grey", text: "FBC unknown" },
} as const;

/** Whether the newest quay-operator FBC catalog references the snapshot's bundle. */
function FBCCatalogLabel({ fbc }: { fbc: FBCCatalog }) {
	const { color, text } = fbcLabels[fbc.status];
	const rows: [string, string][] = [
		["Catalog snapshot", fbc.catalog_snapshot],
		["Catalog bundle", fbc.catalog_bundle_image],
		["Snapshot bundle", fbc.snapshot_bundle_image],
	];
	return (
		<Popover
			headerContent="quay-operator FBC catalog"
			maxWidth="48rem"
			bodyContent={
				<DescriptionList isCompact>
					{rows.map(([term, value]) => (
						<DescriptionListGroup key={term}>
							<DescriptionListTerm>{term}</DescriptionListTerm>
							<DescriptionListDescription>
								{value ? (
									<code style={{ wordBreak: "break-all" }}>{value}</code>
								) : (
									"none"
								)}
							</DescriptionListDescription>
						</DescriptionListGroup>
					))}
				</DescriptionList>
			}
		>
			<Label color={color} isCompact onClick={() => {}}>
				{text}
			</Label>
		</Popover>
	);
}

/** Status of the newest Konflux Release naming the Snapshot; details in the popover. */
export function SnapshotReleaseLabel({
	releases,
}: {
	releases?: KonfluxRelease[];
}) {
	if (!releases?.length) {
		return (
			<Popover
				headerContent="Not submitted for release"
				bodyContent="No Konflux Release names this snapshot. Auto-release is off for every Quay ReleasePlan, so this is the normal state for a build: only ART's shipment and base-image flows create Releases."
			>
				<Label isCompact onClick={() => {}}>
					Not submitted for release
				</Label>
			</Popover>
		);
	}
	const [newest, ...older] = releases;
	const status = releaseStatus(newest);
	const rows: [string, string | undefined][] = [
		["Release CR", newest.name],
		["Release plan", newest.release_plan],
		[
			"Failed at",
			[newest.failed_task, newest.failed_step].filter(Boolean).join("/"),
		],
		[
			"Started",
			newest.start_time && new Date(newest.start_time).toLocaleString(),
		],
		[
			"Completed",
			newest.completion_time &&
				new Date(newest.completion_time).toLocaleString(),
		],
	];
	return (
		<Popover
			headerContent={status.text}
			maxWidth="40rem"
			bodyContent={
				<>
					{"detail" in status && status.detail && (
						<Content component="p">{status.detail}</Content>
					)}
					<DescriptionList isCompact isHorizontal>
						{rows
							.filter(([, value]) => value)
							.map(([term, value]) => (
								<DescriptionListGroup key={term}>
									<DescriptionListTerm>{term}</DescriptionListTerm>
									<DescriptionListDescription>
										<code style={{ wordBreak: "break-all" }}>{value}</code>
									</DescriptionListDescription>
								</DescriptionListGroup>
							))}
					</DescriptionList>
					{status.color === "red" && (
						<HelperText style={{ marginTop: "0.5rem" }}>
							<HelperTextItem>
								The root-cause text is only in the managed PipelineRun in
								rhtap-releng-tenant, which ages out after about 2 hours. Look it
								up from Release CR <code>{newest.name}</code>.
							</HelperTextItem>
						</HelperText>
					)}
					{older.length > 0 && (
						<Content component="p" style={{ marginTop: "0.5rem" }}>
							Earlier Releases:{" "}
							{older
								.map((r) => `${r.name} (${releaseStatus(r).text})`)
								.join(", ")}
						</Content>
					)}
				</>
			}
		>
			<Label color={status.color} isCompact onClick={() => {}}>
				{status.text}
				{older.length > 0 && ` +${older.length}`}
			</Label>
		</Popover>
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
	changed?: ChangedSince;
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

import {
	Card,
	CardBody,
	CardTitle,
	Content,
	EmptyState,
	EmptyStateBody,
	Flex,
	FlexItem,
	Label,
	Pagination,
	Spinner,
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarItem,
} from "@patternfly/react-core";
import {
	ExpandableRowContent,
	Table,
	Tbody,
	Td,
	Th,
	Thead,
	Tr,
} from "@patternfly/react-table";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { getReleaseSnapshot, listReleaseSnapshots } from "../api/client";
import type { KonfluxRelease, ReleaseSnapshot } from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { snapshotFilters } from "../utils/releaseApps";
import ReleasedLabel from "./ReleasedLabel";
import SnapshotComponentsTable from "./SnapshotComponentsTable";

const PAGE_SIZE = 25;

/** Newest-first, paginated Snapshots of a release's applications. */
export default function ReleaseSnapshots({
	version,
	konfluxApp,
}: {
	version: string;
	konfluxApp?: string;
}) {
	const [searchParams, setSearchParams] = useSearchParams();
	const filters = snapshotFilters(konfluxApp);
	const filter =
		filters.find((f) => f.key === searchParams.get("app")) ?? filters[0];
	const withRelease = searchParams.get("with_release") === "true";
	const [page, setPage] = useState(1);
	const [expanded, setExpanded] = useState<Set<string>>(new Set());

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
		setExpanded((prev) => {
			const next = new Set(prev);
			if (!next.delete(name)) next.add(name);
			return next;
		});

	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>Application snapshots</CardTitle>
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
												setParam("app", f.key === "all" ? null : f.key)
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
						<Table variant="compact" aria-label="Application snapshots">
							<Thead>
								<Tr>
									<Th screenReaderText="Row expansion" />
									<Th>Created</Th>
									<Th>Snapshot</Th>
									<Th>Images</Th>
									<Th>Release</Th>
								</Tr>
							</Thead>
							{snapshots.map((s, i) => {
								const isExpanded = expanded.has(s.name);
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
											<Td>{new Date(s.created_at).toLocaleString()}</Td>
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
												<ReleaseStatus releases={s.releases} />
											</Td>
										</Tr>
										<Tr isExpanded={isExpanded}>
											<Td colSpan={5}>
												{isExpanded && (
													<ExpandableRowContent>
														<SnapshotDetail version={version} snapshot={s} />
													</ExpandableRowContent>
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

/** Status of the newest Konflux Release naming the Snapshot. */
function ReleaseStatus({ releases }: { releases?: KonfluxRelease[] }) {
	if (!releases?.length) return <>-</>;
	return (
		<span title={releases[0].name}>
			<ReleasedLabel release={releases[0]} />
			{releases.length > 1 && ` +${releases.length - 1}`}
		</span>
	);
}

function SnapshotDetail({
	version,
	snapshot,
}: {
	version: string;
	snapshot: ReleaseSnapshot;
}) {
	const { data, loading, error } = useCachedFetch(
		snapshot.missing ? null : `releaseSnapshot:${version}:${snapshot.name}`,
		() => getReleaseSnapshot(version, snapshot.name),
	);

	if (snapshot.missing || error?.message.startsWith("404")) {
		return (
			<>
				<EmptyState
					titleText="Snapshot not found"
					headingLevel="h4"
					variant="xs"
				>
					<EmptyStateBody>
						A Konflux Release names this Snapshot, but it is no longer in the
						cluster, so its images are unknown.
					</EmptyStateBody>
				</EmptyState>
				<ReleaseList releases={snapshot.releases} />
			</>
		);
	}
	if (error) return <Content component="p">{error.message}</Content>;
	if (loading || !data) return <Spinner size="md" />;
	return (
		<>
			<ReleaseList releases={data.releases} />
			<SnapshotComponentsTable components={data.components ?? []} />
		</>
	);
}

function ReleaseList({ releases }: { releases?: KonfluxRelease[] }) {
	if (!releases?.length) return null;
	return (
		<Flex
			direction={{ default: "column" }}
			spaceItems={{ default: "spaceItemsXs" }}
			style={{ marginBottom: "0.5rem" }}
		>
			{releases.map((r) => (
				<FlexItem key={r.name}>
					Release <code>{r.name}</code> <ReleasedLabel release={r} />
					{r.completion_time &&
						` completed ${new Date(r.completion_time).toLocaleString()}`}
				</FlexItem>
			))}
		</Flex>
	);
}

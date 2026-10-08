import {
	Card,
	CardBody,
	CardTitle,
	DescriptionList,
	DescriptionListDescription,
	DescriptionListGroup,
	DescriptionListTerm,
	EmptyState,
	EmptyStateBody,
	ExpandableSection,
	Flex,
	FlexItem,
	Gallery,
	Label,
	PageSection,
	Progress,
	ProgressMeasureLocation,
	SearchInput,
	Spinner,
	Switch,
	Title,
	ToggleGroup,
	ToggleGroupItem,
	Toolbar,
	ToolbarContent,
	ToolbarGroup,
	ToolbarItem,
} from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	ListIcon,
	ThIcon,
} from "@patternfly/react-icons";
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { listReleasesOverview } from "../api/client";
import type {
	IssueSummary,
	ReadinessResponse,
	ReleaseOverview,
	ReleaseVersion,
} from "../api/types";
import { seedCache, useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";

type SignalFilter = "all" | "red" | "yellow" | "green";
type ViewMode = "compact" | "expanded";

export default function ReleasesOverview() {
	const [searchParams, setSearchParams] = useSearchParams();
	const query = searchParams.get("q") ?? "";
	const signalFilter = (searchParams.get("signal") ?? "all") as SignalFilter;
	const viewMode = (searchParams.get("view") ?? "compact") as ViewMode;
	const showAll = searchParams.get("all") === "1";

	const config = useConfig();

	const { data: overviews, loading } = useCachedFetch(
		"releasesOverview",
		listReleasesOverview,
	);

	// Seed individual cache entries so detail pages can reuse data
	useEffect(() => {
		if (!overviews) return;
		for (const ov of overviews) {
			seedCache(`issueSummary:${ov.release.name}`, ov.issue_summary);
			seedCache(`readiness:${ov.release.name}`, ov.readiness);
		}
	}, [overviews]);

	const [releasedExpanded, setReleasedExpanded] = useState(false);

	const setParam = (key: string, value: string) => {
		setSearchParams((prev) => {
			const next = new URLSearchParams(prev);
			if (value && value !== "all" && value !== "compact") {
				next.set(key, value);
			} else {
				next.delete(key);
			}
			return next;
		});
	};

	if (loading && !overviews) {
		return (
			<PageSection>
				<div style={{ textAlign: "center" }}>
					<Spinner />
				</div>
			</PageSection>
		);
	}

	const overviewList = [...(overviews ?? [])].sort((a, b) =>
		b.release.name.localeCompare(a.release.name, undefined, { numeric: true }),
	);
	// By default only the next z-release of each stream; the rest stay
	// behind the "show all" switch.
	const active = overviewList.filter(
		(ov) => !ov.release.released && (showAll || ov.next_in_stream),
	);
	const released = overviewList.filter((ov) => ov.release.released);

	if (overviewList.length === 0) {
		return (
			<PageSection>
				<EmptyState>
					<Title headingLevel="h2" size="lg">
						No releases found
					</Title>
					<EmptyStateBody>
						No active releases discovered from JIRA. Ensure the JIRA sync is
						configured and release tickets with component
						&quot;-area/release&quot; exist.
					</EmptyStateBody>
				</EmptyState>
			</PageSection>
		);
	}

	const galleryMinWidth = viewMode === "compact" ? "300px" : "400px";

	const filterOverview = (ov: ReleaseOverview): boolean => {
		const displayName = formatReleaseName(ov.release.name);
		if (
			query &&
			!displayName.toLowerCase().includes(query.toLowerCase()) &&
			!ov.release.name.toLowerCase().includes(query.toLowerCase())
		) {
			return false;
		}
		if (signalFilter !== "all" && ov.readiness.signal !== signalFilter) {
			return false;
		}
		return true;
	};

	return (
		<PageSection>
			<Toolbar>
				<ToolbarContent>
					<ToolbarItem>
						<SearchInput
							placeholder="Filter releases..."
							value={query}
							onChange={(_e, val) => setParam("q", val)}
							onClear={() => setParam("q", "")}
						/>
					</ToolbarItem>
					<ToolbarItem>
						<ToggleGroup aria-label="Signal filter">
							{(["all", "red", "yellow", "green"] as const).map((s) => (
								<ToggleGroupItem
									key={s}
									text={s.charAt(0).toUpperCase() + s.slice(1)}
									isSelected={signalFilter === s}
									onChange={() => setParam("signal", s)}
								/>
							))}
						</ToggleGroup>
					</ToolbarItem>
					<ToolbarItem>
						<Switch
							id="show-all-releases"
							label="Show all open release tickets"
							isChecked={showAll}
							onChange={(_e, checked) => setParam("all", checked ? "1" : "")}
						/>
					</ToolbarItem>
					<ToolbarGroup align={{ default: "alignEnd" }}>
						<ToolbarItem>
							<ToggleGroup aria-label="View mode">
								<ToggleGroupItem
									icon={<ListIcon />}
									aria-label="Compact view"
									isSelected={viewMode === "compact"}
									onChange={() => setParam("view", "compact")}
								/>
								<ToggleGroupItem
									icon={<ThIcon />}
									aria-label="Expanded view"
									isSelected={viewMode === "expanded"}
									onChange={() => setParam("view", "expanded")}
								/>
							</ToggleGroup>
						</ToolbarItem>
					</ToolbarGroup>
				</ToolbarContent>
			</Toolbar>

			<Gallery hasGutter minWidths={{ default: galleryMinWidth }}>
				{active.filter(filterOverview).map((ov) => (
					<ReleaseCard
						key={ov.release.name}
						release={ov.release}
						issueSummary={ov.issue_summary}
						readinessSignal={ov.readiness}
						componentCount={ov.component_count}
						latestBuild={ov.latest_build}
						shipped={ov.shipped}
						viewMode={viewMode}
						jiraBaseUrl={config?.jira_base_url}
					/>
				))}
			</Gallery>

			{released.length > 0 && (
				<ExpandableSection
					toggleText={`Released (${released.length})`}
					isExpanded={releasedExpanded}
					onToggle={(_e, val) => setReleasedExpanded(val)}
					style={{ marginTop: "1.5rem" }}
				>
					<Gallery hasGutter minWidths={{ default: galleryMinWidth }}>
						{released.filter(filterOverview).map((ov) => (
							<ReleaseCard
								key={ov.release.name}
								release={ov.release}
								issueSummary={ov.issue_summary}
								readinessSignal={ov.readiness}
								componentCount={ov.component_count}
								latestBuild={ov.latest_build}
								shipped={ov.shipped}
								viewMode={viewMode}
								jiraBaseUrl={config?.jira_base_url}
							/>
						))}
					</Gallery>
				</ExpandableSection>
			)}
		</PageSection>
	);
}

function ReleaseCard({
	release,
	issueSummary,
	readinessSignal,
	componentCount,
	latestBuild,
	shipped,
	viewMode,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	issueSummary?: IssueSummary;
	readinessSignal?: ReadinessResponse;
	componentCount: number;
	latestBuild?: string;
	shipped: boolean;
	viewMode: ViewMode;
	jiraBaseUrl?: string;
}) {
	const dueDate = release.due_date ? new Date(release.due_date) : null;
	const releaseDate = release.release_date
		? new Date(release.release_date)
		: null;
	const targetDate = dueDate ?? releaseDate;

	const signalColor = readinessSignal?.signal ?? "grey";
	const signalIcon =
		signalColor === "green" ? (
			<CheckCircleIcon />
		) : signalColor === "red" ? (
			<ExclamationCircleIcon />
		) : signalColor === "yellow" ? (
			<ExclamationTriangleIcon />
		) : undefined;

	const verifiedPercent =
		issueSummary && issueSummary.total > 0
			? Math.round((issueSummary.verified / issueSummary.total) * 100)
			: 0;

	const builds =
		componentCount > 0
			? `${componentCount} components${latestBuild ? `, latest ${new Date(latestBuild).toLocaleDateString()}` : ""}`
			: "None yet";

	const navigate = useNavigate();
	const displayName = formatReleaseName(release.name);

	const ticketLink =
		release.release_ticket_key && jiraBaseUrl
			? jiraIssueUrl(release.release_ticket_key, jiraBaseUrl)
			: null;

	return (
		<Card
			isCompact
			isClickable
			style={{ cursor: "pointer" }}
			onClick={(e) => {
				if ((e.target as HTMLElement).closest("a, button")) return;
				navigate(`/releases/${encodeURIComponent(release.name)}`);
			}}
		>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<FlexItem>
						{displayName}
						{shipped && !release.released && (
							<Label isCompact color="blue" style={{ marginLeft: "0.5rem" }}>
								Shipped, ticket still open
							</Label>
						)}
					</FlexItem>
					<FlexItem>
						{readinessSignal && (
							<Label
								color={
									signalColor === "green"
										? "green"
										: signalColor === "red"
											? "red"
											: signalColor === "yellow"
												? "yellow"
												: "grey"
								}
								icon={signalIcon}
								isCompact
							>
								{readinessSignal.message}
							</Label>
						)}
					</FlexItem>
				</Flex>
			</CardTitle>
			<CardBody>
				{viewMode === "compact" ? (
					<DescriptionList isCompact isHorizontal>
						{targetDate && (
							<DescriptionListGroup>
								<DescriptionListTerm>Target</DescriptionListTerm>
								<DescriptionListDescription>
									{targetDate.toLocaleDateString()}
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
						<DescriptionListGroup>
							<DescriptionListTerm>Builds</DescriptionListTerm>
							<DescriptionListDescription>{builds}</DescriptionListDescription>
						</DescriptionListGroup>
						{release.release_ticket_key && (
							<DescriptionListGroup>
								<DescriptionListTerm>Ticket</DescriptionListTerm>
								<DescriptionListDescription>
									{ticketLink ? (
										<a
											href={ticketLink}
											target="_blank"
											rel="noopener noreferrer"
										>
											{release.release_ticket_key}
										</a>
									) : (
										release.release_ticket_key
									)}
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
					</DescriptionList>
				) : (
					<Flex direction={{ default: "column" }}>
						<FlexItem>
							<Flex justifyContent={{ default: "justifyContentSpaceBetween" }}>
								{targetDate && (
									<FlexItem>
										<span className="rr-label">Target Date</span>
										<div>{targetDate.toLocaleDateString()}</div>
									</FlexItem>
								)}
								{release.release_ticket_key && (
									<FlexItem>
										<span className="rr-label">Ticket</span>
										<div>
											{ticketLink ? (
												<a
													href={ticketLink}
													target="_blank"
													rel="noopener noreferrer"
												>
													{release.release_ticket_key}
												</a>
											) : (
												release.release_ticket_key
											)}
										</div>
									</FlexItem>
								)}
								<FlexItem>
									<span className="rr-label">Builds</span>
									<div>{builds}</div>
								</FlexItem>
								{issueSummary && issueSummary.cves > 0 && (
									<FlexItem>
										<span className="rr-label">CVEs</span>
										<div>{issueSummary.cves}</div>
									</FlexItem>
								)}
							</Flex>
						</FlexItem>
						{issueSummary && issueSummary.total > 0 && (
							<FlexItem>
								<Progress
									value={verifiedPercent}
									title={`${issueSummary.verified}/${issueSummary.total} verified`}
									measureLocation={ProgressMeasureLocation.outside}
									size="sm"
								/>
							</FlexItem>
						)}
					</Flex>
				)}
			</CardBody>
		</Card>
	);
}

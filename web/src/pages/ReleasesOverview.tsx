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
	Flex,
	FlexItem,
	Gallery,
	Label,
	PageSection,
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
import { Link, useSearchParams } from "react-router-dom";
import { listReleasesOverview } from "../api/client";
import type {
	IssueSummary,
	ReadinessResponse,
	ReleaseVersion,
	ShipmentMR,
} from "../api/types";
import LoadError from "../components/LoadError";
import { ShipmentLabels } from "../components/ShipmentLabels";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { day } from "../utils/format";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";

type ViewMode = "compact" | "expanded";

const muted = { color: "var(--pf-t--global--text--color--subtle)" };

export default function ReleasesOverview() {
	const [searchParams, setSearchParams] = useSearchParams();
	const viewMode = (searchParams.get("view") ?? "compact") as ViewMode;
	const showAll = searchParams.get("all") === "1";

	const config = useConfig();

	const {
		data: overviews,
		loading,
		error,
		refetch,
	} = useCachedFetch("releasesOverview", listReleasesOverview);

	const setParam = (key: string, value: string) => {
		setSearchParams((prev) => {
			const next = new URLSearchParams(prev);
			if (value && value !== "compact") {
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
				<EmptyState icon={Spinner} titleText="Loading releases…" />
			</PageSection>
		);
	}

	if (!overviews && error) {
		return (
			<PageSection>
				<LoadError title="Could not load releases" onRetry={refetch}>
					The server did not return the release list. Try again in a moment.
				</LoadError>
			</PageSection>
		);
	}

	const overviewList = [...(overviews ?? [])].sort((a, b) =>
		b.release.name.localeCompare(a.release.name, undefined, { numeric: true }),
	);
	// Only the next z-release of each stream, unless every version is asked for.
	const visible = showAll
		? overviewList
		: overviewList.filter((ov) => ov.next_in_stream);

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

	return (
		<PageSection>
			<Title
				headingLevel="h1"
				size="lg"
				style={{ marginBottom: "var(--pf-t--global--spacer--md)" }}
			>
				Releases
			</Title>
			<Toolbar>
				<ToolbarContent alignItems="center">
					<ToolbarItem>
						<Switch
							id="show-all-versions"
							label="Show all versions"
							isChecked={showAll}
							onChange={(_e, checked) => setParam("all", checked ? "1" : "")}
						/>
					</ToolbarItem>
					{!showAll && (
						<ToolbarItem style={muted}>Next version in each stream</ToolbarItem>
					)}
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

			<Gallery
				component="ul"
				hasGutter
				minWidths={{ default: galleryMinWidth }}
			>
				{visible.map((ov) => (
					<ReleaseCard
						key={ov.release.name}
						release={ov.release}
						issueSummary={ov.issue_summary}
						readinessSignal={ov.readiness}
						latestBuild={ov.latest_build}
						shipmentMR={ov.shipment_mr}
						viewMode={viewMode}
						jiraBaseUrl={config?.jira_base_url}
					/>
				))}
			</Gallery>
		</PageSection>
	);
}

function ReleaseCard({
	release,
	issueSummary,
	readinessSignal,
	latestBuild,
	shipmentMR,
	viewMode,
	jiraBaseUrl,
}: {
	release: ReleaseVersion;
	issueSummary?: IssueSummary;
	readinessSignal: ReadinessResponse;
	latestBuild?: string;
	shipmentMR: ShipmentMR | null;
	viewMode: ViewMode;
	jiraBaseUrl?: string;
}) {
	const target = release.due_date ?? release.release_date;
	const targetDate = target && day(target);

	const signalIcons = {
		green: <CheckCircleIcon />,
		red: <ExclamationCircleIcon />,
		yellow: <ExclamationTriangleIcon />,
	};

	// A build time, so its local date, in the same format as the target date.
	const lastBuild = latestBuild
		? new Date(latestBuild).toLocaleDateString("en-US", {
				month: "short",
				day: "numeric",
				year: "numeric",
			})
		: "No image snapshot yet";

	const displayName = formatReleaseName(release.name);

	const ticket =
		release.release_ticket_key && jiraBaseUrl ? (
			<a
				href={jiraIssueUrl(release.release_ticket_key, jiraBaseUrl)}
				target="_blank"
				rel="noopener noreferrer"
				style={{ textDecoration: "none" }}
			>
				{release.release_ticket_key}
			</a>
		) : (
			release.release_ticket_key
		);

	return (
		<Card component="li" isCompact>
			<CardTitle>
				<Flex
					justifyContent={{ default: "justifyContentSpaceBetween" }}
					alignItems={{ default: "alignItemsCenter" }}
				>
					<FlexItem>
						<Title headingLevel="h2" size="md">
							<Link to={`/releases/${encodeURIComponent(release.name)}`}>
								{displayName}
							</Link>
						</Title>
					</FlexItem>
					<Flex spaceItems={{ default: "spaceItemsSm" }}>
						{shipmentMR && (
							<FlexItem>
								<ShipmentLabels mr={shipmentMR} />
							</FlexItem>
						)}
						{readinessSignal.signal && (
							<FlexItem>
								<Label
									color={readinessSignal.signal}
									icon={signalIcons[readinessSignal.signal]}
									isCompact
								>
									{readinessSignal.message}
								</Label>
							</FlexItem>
						)}
					</Flex>
				</Flex>
			</CardTitle>
			<CardBody>
				{viewMode === "compact" ? (
					<DescriptionList isCompact isHorizontal>
						{targetDate && (
							<DescriptionListGroup>
								<DescriptionListTerm>Target</DescriptionListTerm>
								<DescriptionListDescription>
									{targetDate}
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
						<DescriptionListGroup>
							<DescriptionListTerm>Image snapshot</DescriptionListTerm>
							<DescriptionListDescription>
								{lastBuild}
							</DescriptionListDescription>
						</DescriptionListGroup>
						{release.release_ticket_key && (
							<DescriptionListGroup>
								<DescriptionListTerm>Ticket</DescriptionListTerm>
								<DescriptionListDescription>
									{ticket}
								</DescriptionListDescription>
							</DescriptionListGroup>
						)}
					</DescriptionList>
				) : (
					<Flex justifyContent={{ default: "justifyContentSpaceBetween" }}>
						{targetDate && (
							<FlexItem>
								<span className="rr-label">Target</span>
								<div>{targetDate}</div>
							</FlexItem>
						)}
						{release.release_ticket_key && (
							<FlexItem>
								<span className="rr-label">Ticket</span>
								<div>{ticket}</div>
							</FlexItem>
						)}
						<FlexItem>
							<span className="rr-label">Image snapshot</span>
							<div>{lastBuild}</div>
						</FlexItem>
						{issueSummary && issueSummary.cves > 0 && (
							<FlexItem>
								<span className="rr-label">CVEs</span>
								<div>{issueSummary.cves}</div>
							</FlexItem>
						)}
					</Flex>
				)}
			</CardBody>
		</Card>
	);
}

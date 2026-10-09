import {
	Breadcrumb,
	BreadcrumbItem,
	HelperText,
	HelperTextItem,
	PageSection,
	Spinner,
	Title,
} from "@patternfly/react-core";
import { Link, useParams } from "react-router-dom";
import { getRelease } from "../api/client";
import BuildAttempts from "../components/BuildAttempts";
import { UnlinkedProwRuns } from "../components/ProwRuns";
import {
	LatestSnapshot,
	SnapshotHistory,
	useLatestSnapshot,
} from "../components/ReleaseSnapshots";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { formatReleaseName } from "../utils/links";
import { versionNumber } from "./readiness";
import Switcher, { mockupPath, usePicks } from "./Switcher";

/** The Builds page as Q1-A makes it: the stream cards above the history. */
export default function BuildsMockup() {
	const { version } = useParams<{ version: string }>();
	const picks = usePicks();
	const { data: release, loading } = useCachedFetch(
		version ? `release:${version}` : null,
		() => getRelease(version!),
	);
	const latest = useLatestSnapshot(version!, release);
	const app = release?.konflux_application;
	const minor = versionNumber(version!).split(".").slice(0, 2).join(".");

	return (
		<>
			<PageSection>
				<Switcher version={version!} picks={picks} builds />
			</PageSection>
			<PageSection>
				<Breadcrumb style={{ marginBottom: "1rem" }}>
					<BreadcrumbItem>
						<Link to="/">Releases</Link>
					</BreadcrumbItem>
					<BreadcrumbItem>
						<Link to={mockupPath(version!, picks)}>
							{formatReleaseName(version!)}
						</Link>
					</BreadcrumbItem>
					<BreadcrumbItem isActive>Builds</BreadcrumbItem>
				</Breadcrumb>
				{!release ? (
					loading ? (
						<Spinner />
					) : (
						`No data found for release "${version}".`
					)
				) : (
					<>
						<Title headingLevel="h1">Builds of {app || "this release"}</Title>
						<HelperText style={{ marginBottom: "1rem" }}>
							<HelperTextItem>
								The stream: every {minor}.z build, the same for each {minor}{" "}
								version. Snapshots, ART image builds and CI runs.
							</HelperTextItem>
						</HelperText>
						<LatestSnapshot version={version!} state={latest} />
						<BuildAttempts version={version!} />
						<SnapshotHistory version={version!} konfluxApp={app} />
						<UnlinkedProwRuns version={version!} />
					</>
				)}
			</PageSection>
		</>
	);
}

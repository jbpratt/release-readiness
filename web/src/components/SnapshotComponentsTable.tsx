import { ClipboardCopy, Tooltip } from "@patternfly/react-core";
import {
	CircleIcon,
	ExternalLinkAltIcon,
	InProgressIcon,
} from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type {
	ArtBuild,
	PendingArtBuild,
	SnapshotImage,
	SnapshotProwRuns,
} from "../api/types";
import { type ComponentField, componentFields } from "../utils/componentDiff";
import { quayManifestUrl, upstreamCommitUrl } from "../utils/links";
import { ProwRunBadge } from "./ProwRuns";

const external = { target: "_blank", rel: "noopener noreferrer" };

const linkIcon = (
	<ExternalLinkAltIcon style={{ fontSize: "0.75em", marginLeft: "0.25rem" }} />
);

/** Changed fields per component vs the last released snapshot, named by `since`. */
export type ChangedSince = {
	since: string;
	fields: Map<string, ComponentField[]>;
};

export const changedDot = (
	<CircleIcon
		style={{
			fontSize: "0.5em",
			color: "var(--pf-t--global--icon--color--status--info--default)",
		}}
	/>
);

function Unknown({ why }: { why: string }) {
	return (
		<Tooltip content={why}>
			<span>unknown</span>
		</Tooltip>
	);
}

const noArtRecord = <Unknown why="No ART build record found" />;

/** A commit, linked when its repo is on github.com. */
function CommitLink({ repo, sha }: { repo: string; sha: string }) {
	if (!sha) return <Unknown why="No commit recorded" />;
	const url = upstreamCommitUrl(repo, sha);
	const text = (
		<code style={{ fontSize: "0.85em" }}>{sha.substring(0, 12)}</code>
	);
	return (
		<Tooltip content={`${repo} ${sha}`}>
			{url ? (
				<a href={url} {...external}>
					{text}
				</a>
			) : (
				text
			)}
		</Tooltip>
	);
}

/** The image's ART build record; that page links its logs and pipeline run. */
function ArtBuildLink({ art }: { art: ArtBuild | null }) {
	if (!art) return noArtRecord;
	return (
		<Tooltip content="Opens ART build history: the build record, its logs and pipeline run">
			<a href={art.build_url} {...external}>
				ART build{linkIcon}
			</a>
		</Tooltip>
	);
}

/** A newer ART build of the component, not yet in this Snapshot. */
function PendingArtBuildLink({ pending }: { pending: PendingArtBuild | null }) {
	if (!pending) return null;
	return (
		<div style={{ fontSize: "0.85em" }}>
			<Tooltip
				content={`Started ${new Date(pending.started_at).toLocaleString()}; opens its ART build record`}
			>
				<a href={pending.build_url} {...external}>
					<InProgressIcon style={{ marginRight: "0.25rem" }} />
					Newer build in progress from{" "}
					<code>{pending.upstream_sha.substring(0, 12)}</code>
					{linkIcon}
				</a>
			</Tooltip>
		</div>
	);
}

/** Digest-pinned image reference, linked to its quay.io manifest page. */
function ImageDigestLink({ image }: { image: string }) {
	const url = quayManifestUrl(image);
	const digest = image.split("@")[1];
	const text = (
		<code style={{ fontSize: "0.85em" }}>
			{digest ? digest.replace(/^(sha256:.{12}).*/, "$1") : image}
		</code>
	);
	return (
		<Tooltip content={url ? `Opens on quay.io: ${image}` : image}>
			{url ? (
				<a href={url} {...external}>
					{text}
					{linkIcon}
				</a>
			) : (
				text
			)}
		</Tooltip>
	);
}

/** The component images exactly as one Snapshot records them. */
export default function SnapshotComponentsTable({
	components,
	prowRuns,
	changed,
}: {
	components: SnapshotImage[];
	changed?: ChangedSince;
	/** Shown only once a CI job of the release has synced. */
	prowRuns?: SnapshotProwRuns;
}) {
	const ci = prowRuns?.last_successful_sync ? prowRuns : undefined;
	return (
		<Table variant="compact" aria-label="Snapshot components">
			<Thead>
				<Tr>
					<Th width={20}>Component</Th>
					<Th width={25}>NVR</Th>
					<Th width={15}>Image</Th>
					<Th
						width={10}
						modifier="nowrap"
						info={{ tooltip: "Commit in the public upstream repo" }}
					>
						Upstream SHA
					</Th>
					<Th width={10}>ART</Th>
					{ci && <Th modifier="fitContent">Periodic CI</Th>}
				</Tr>
			</Thead>
			<Tbody>
				{components.map((c) => {
					const fields = changed?.fields.get(c.name) ?? [];
					const mark = (f: ComponentField) =>
						fields.includes(f) && <>{changedDot} </>;
					const what = `Changed vs ${changed?.since}, last released: ${fields.map((f) => componentFields[f]).join(", ")}`;
					return (
						<Tr key={c.name}>
							<Td>
								{changed && (
									<span style={{ display: "inline-block", width: "1rem" }}>
										{fields.length > 0 && (
											<Tooltip content={what}>
												<span role="img" aria-label={what}>
													{changedDot}
												</span>
											</Tooltip>
										)}
									</span>
								)}
								{c.name}
							</Td>
							<Td>
								{mark("nvr")}
								{!c.art ? (
									noArtRecord
								) : c.art.nvr ? (
									<code style={{ fontSize: "0.85em" }}>{c.art.nvr}</code>
								) : (
									<Unknown why="ART build record has no NVR" />
								)}
							</Td>
							<Td>
								{mark("digest")}
								<ImageDigestLink image={c.image} />
								<ClipboardCopy
									variant="inline-compact"
									isCode
									truncation
									hoverTip="Copy pullspec"
									clickTip="Copied"
								>
									{c.image}
								</ClipboardCopy>
							</Td>
							<Td>
								{mark("upstream")}
								{c.art ? (
									<CommitLink
										repo={c.art.upstream_repo}
										sha={c.art.upstream_sha}
									/>
								) : (
									noArtRecord
								)}
							</Td>
							<Td>
								<ArtBuildLink art={c.art} />
								<PendingArtBuildLink pending={c.pending_art_build} />
							</Td>
							{ci && (
								<Td modifier="fitContent">
									<ProwRunBadge
										component={c.name}
										image={c.image}
										runs={ci.components[c.name] ?? []}
										sync={ci}
									/>
								</Td>
							)}
						</Tr>
					);
				})}
			</Tbody>
		</Table>
	);
}

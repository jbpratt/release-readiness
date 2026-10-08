import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import type { SnapshotImage } from "../api/types";
import { quayManifestUrl } from "../utils/links";
import GitShaLink from "./GitShaLink";

/** Digest-pinned image reference, linked to its quay.io manifest page. */
export function ImageDigestLink({ image }: { image: string }) {
	const url = quayManifestUrl(image);
	const digest = image.split("@")[1];
	const text = (
		<code style={{ fontSize: "0.85em" }} title={image}>
			{digest ? digest.replace(/^(sha256:.{12}).*/, "$1") : image}
		</code>
	);
	return url ? (
		<a href={url} target="_blank" rel="noopener noreferrer">
			{text}
		</a>
	) : (
		text
	);
}

/** The component images exactly as one Snapshot records them. */
export default function SnapshotComponentsTable({
	components,
}: {
	components: SnapshotImage[];
}) {
	return (
		<Table variant="compact" aria-label="Snapshot components">
			<Thead>
				<Tr>
					<Th>Component</Th>
					<Th>Image</Th>
					<Th>Git SHA</Th>
				</Tr>
			</Thead>
			<Tbody>
				{components.map((c) => (
					<Tr key={c.name}>
						<Td>{c.name}</Td>
						<Td>
							<ImageDigestLink image={c.image} />
						</Td>
						<Td>
							<GitShaLink sha={c.git_sha} gitUrl={c.git_url} />
						</Td>
					</Tr>
				))}
			</Tbody>
		</Table>
	);
}

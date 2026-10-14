import { Card, CardBody, CardTitle } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useSearchParams } from "react-router-dom";
import type { ReleaseCandidate } from "../api/types";
import { BuiltCell, CveCell, Flags, muted } from "../components/OperatorImages";
import { candidateOperators, releaseImages } from "../utils/releaseDetail";
import { FbcImagesPage, ShipCell } from "./fbcImages";

/** Design A: one table of the release's images, each once. */
export default function FbcImagesA() {
	return (
		<FbcImagesPage
			ui={{ card: (d, version) => <ImagesCard d={d} version={version} /> }}
		/>
	);
}

/** The release's images, the operator filter's only when set. */
function ImagesCard({ d, version }: { d: ReleaseCandidate; version: string }) {
	const [params] = useSearchParams();
	const operator = params.get("operator") ?? "";
	const images = releaseImages(
		candidateOperators(d).filter((o) => !operator || o.operator === operator),
		version,
	);
	return (
		<Card isCompact style={{ marginBottom: "1rem" }}>
			<CardTitle>
				Images ({images.length}){operator && ` of ${operator}`}
			</CardTitle>
			<CardBody>
				<Table variant="compact" aria-label="Release images">
					<Thead>
						<Tr>
							<Th>Image</Th>
							<Th>Built (UTC)</Th>
							<Th>Commit</Th>
							<Th>Pushed to</Th>
							<Th>Fixable CVEs</Th>
							<Th>Issues</Th>
						</Tr>
					</Thead>
					<Tbody>
						{images.map((r) => (
							<Tr key={r.image.digest}>
								<Td dataLabel="Image">
									<div>{r.label}</div>
									<div style={{ ...muted, fontSize: "0.85rem" }}>
										{r.operators.join(", ")}
									</div>
								</Td>
								<Td dataLabel="Built (UTC)" modifier="nowrap">
									<BuiltCell image={r.image} />
								</Td>
								<Td dataLabel="Commit">
									<code title={r.image.commit}>
										{r.image.commit.slice(0, 7) || "–"}
									</code>
								</Td>
								<Td dataLabel="Pushed to">
									<ShipCell image={r.image} />
								</Td>
								<Td dataLabel="Fixable CVEs">
									<CveCell image={r.image} />
								</Td>
								<Td dataLabel="Issues">
									<Flags flags={r.issues} />
								</Td>
							</Tr>
						))}
					</Tbody>
				</Table>
			</CardBody>
		</Card>
	);
}

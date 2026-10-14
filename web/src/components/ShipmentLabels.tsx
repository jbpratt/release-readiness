import { Label, LabelGroup } from "@patternfly/react-core";
import type { ShipmentMR } from "../api/types";
import { shipmentChip, shipmentFlags } from "../utils/shipment";
import { external } from "./OperatorImages";

/** A version's ART shipment MR: its stage chip, linking the MR, and flags. */
export function ShipmentLabels({ mr }: { mr: ShipmentMR }) {
	const chip = shipmentChip(mr);
	return (
		<LabelGroup>
			<Label
				isCompact
				status={chip.status}
				render={({ className, content, componentRef }) => (
					<a
						className={className}
						href={mr.web_url}
						ref={componentRef}
						title={chip.tooltip}
						{...external}
					>
						{content}
					</a>
				)}
			>
				{chip.text}
			</Label>
			{shipmentFlags(mr).map((f) => (
				<Label key={f.text} isCompact status={f.status}>
					{f.text}
				</Label>
			))}
		</LabelGroup>
	);
}

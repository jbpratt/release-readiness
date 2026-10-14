import { Label, LabelGroup, type LabelProps } from "@patternfly/react-core";
import type { ShipmentMR } from "../api/types";
import { shipmentChips } from "../utils/shipment";
import { external } from "./OperatorImages";

/** A Label render prop that makes the label a link to the shipment MR. */
export function mrLink(mr: ShipmentMR, title?: string): LabelProps["render"] {
	return ({ className, content, componentRef }) => (
		<a
			className={className}
			href={mr.web_url}
			ref={componentRef}
			title={title}
			{...external}
		>
			{content}
		</a>
	);
}

/** A version's ART shipment MR chips: see shipmentChips. */
export function ShipmentLabels({
	mr,
	shipped,
}: {
	mr: ShipmentMR;
	shipped: boolean;
}) {
	return (
		<LabelGroup>
			{shipmentChips(mr, shipped).map((c) => (
				<Label
					key={c.text}
					isCompact
					status={c.status}
					render={c.link ? mrLink(mr, c.tooltip) : undefined}
				>
					{c.text}
				</Label>
			))}
		</LabelGroup>
	);
}

import {
	HelperText,
	HelperTextItem,
	Icon,
	Label,
	type LabelProps,
} from "@patternfly/react-core";
import { CheckCircleIcon, InfoCircleIcon } from "@patternfly/react-icons";
import type { ShipmentMR } from "../api/types";
import { shipmentFlags, shipmentLabel } from "../utils/shipment";
import { external, muted } from "./OperatorImages";

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

/** A version's ART shipment MR label, linking it, and its flags as text. */
export function ShipmentLabels({
	mr,
	shipped,
}: {
	mr: ShipmentMR;
	shipped: boolean;
}) {
	const label = shipmentLabel(mr, shipped);
	const flags = shipped ? [] : shipmentFlags(mr);
	return (
		<>
			<Label isCompact status={label.status} render={mrLink(mr, label.tooltip)}>
				{label.text}
			</Label>
			{flags.length > 0 && (
				<HelperText>
					<HelperTextItem>{flags.join(" · ")}</HelperTextItem>
				</HelperText>
			)}
		</>
	);
}

/** The shipment MR's state as its icon and a link naming it, no filled label. */
export function ShipmentLink({
	mr,
	shipped,
}: {
	mr: ShipmentMR;
	shipped: boolean;
}) {
	const label = shipmentLabel(mr, shipped);
	const flags = shipped ? [] : shipmentFlags(mr);
	return (
		<span
			style={{ display: "inline-flex", gap: "0.5rem", alignItems: "center" }}
		>
			<Icon status={label.status} isInline>
				{label.status === "success" ? <CheckCircleIcon /> : <InfoCircleIcon />}
			</Icon>
			<span>
				<a href={mr.web_url} title={label.tooltip} {...external}>
					{label.text.replace(/^Shipment MR:? ?/, "MR ")}
				</a>
				{flags.length > 0 && (
					<span style={muted}>{` · ${flags.join(" · ")}`}</span>
				)}
			</span>
		</span>
	);
}

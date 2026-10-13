import { Flex, FlexItem, Icon } from "@patternfly/react-core";
import {
	CheckCircleIcon,
	ExclamationCircleIcon,
	ExclamationTriangleIcon,
	InProgressIcon,
} from "@patternfly/react-icons";
import type { ReactNode } from "react";
import type { StatusLine } from "../utils/releaseDetail";

const statusIcons: Record<StatusLine["status"], ReactNode> = {
	done: (
		<Icon status="success" isInline>
			<CheckCircleIcon />
		</Icon>
	),
	failed: (
		<Icon status="danger" isInline>
			<ExclamationCircleIcon />
		</Icon>
	),
	running: (
		<Icon status="info" isInline>
			<InProgressIcon />
		</Icon>
	),
	warning: (
		<Icon status="warning" isInline>
			<ExclamationTriangleIcon />
		</Icon>
	),
};

/** A status icon, then its text, which wraps clear of it. */
export function Status({
	status,
	children,
}: {
	status: StatusLine["status"];
	children: ReactNode;
}) {
	return (
		<Flex gap={{ default: "gapSm" }} flexWrap={{ default: "nowrap" }}>
			<FlexItem>{statusIcons[status]}</FlexItem>
			<FlexItem>{children}</FlexItem>
		</Flex>
	);
}

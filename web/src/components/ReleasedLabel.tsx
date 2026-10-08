import { Label } from "@patternfly/react-core";
import type { KonfluxRelease } from "../api/types";

type LabelColor = "green" | "red" | "yellow" | "blue";

const releasedColors: Record<string, LabelColor> = {
	True: "green",
	False: "red",
	Unknown: "yellow",
};

const reasonColors: Record<string, LabelColor> = {
	Succeeded: "green",
	Failed: "red",
	Progressing: "blue",
};

export default function ReleasedLabel({
	release,
}: {
	release: KonfluxRelease;
}) {
	if (!release.released_status) return <>-</>;
	return (
		<Label
			color={
				reasonColors[release.released_reason] ??
				releasedColors[release.released_status] ??
				"grey"
			}
			isCompact
		>
			{release.released_reason || release.released_status}
		</Label>
	);
}

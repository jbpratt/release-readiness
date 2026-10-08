import {
	ClipboardCopy,
	clipboardCopyFunc,
	Tooltip,
} from "@patternfly/react-core";
import { gitRepoName } from "../utils/links";

// The build repo is a private ART mirror, so the sha is copyable rather than linked.
export default function GitShaLink({
	sha,
	gitUrl,
}: {
	sha: string;
	gitUrl?: string;
}) {
	if (!sha) return null;
	const repo = gitUrl ? gitRepoName(gitUrl) : null;
	let tip = "Build repo commit";
	if (repo) {
		tip += ` in ${repo}`;
		if (repo.startsWith("openshift-priv/")) tip += " (private ART mirror)";
	}
	return (
		<Tooltip content={tip}>
			<span>
				<ClipboardCopy
					variant="inline-compact"
					isCode
					onCopy={(e) => clipboardCopyFunc(e, sha)}
				>
					{sha.substring(0, 12)}
				</ClipboardCopy>
			</span>
		</Tooltip>
	);
}

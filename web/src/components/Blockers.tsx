import { List, ListItem } from "@patternfly/react-core";
import { Link } from "react-router-dom";
import { type BlockerGroup, type BlockerLink, where } from "../utils/blockers";
import { external, muted } from "./OperatorImages";

/** Grouped blockers, each with what, where, owner, the next step and links. */
export function Blockers({ groups }: { groups: BlockerGroup[] }) {
	return (
		<List isPlain isBordered>
			{groups.map((b) => (
				<ListItem key={`${b.level} ${b.code} ${b.operator} ${b.why}`}>
					<Entry b={b} />
				</ListItem>
			))}
		</List>
	);
}

/** What blocks and why; where and who owns it; the next step and links. */
function Entry({ b }: { b: BlockerGroup }) {
	return (
		<>
			<div>
				<strong>{b.what}</strong>
				{b.count > 1 && ` (${b.count})`}
				{b.why && <span> · {b.why}</span>}
			</div>
			<div style={muted}>
				{where(b)}
				{b.owner && ` · Owner: ${b.owner}`}
			</div>
			{(b.action || b.links.length > 0) && (
				<div>
					{b.action && <>Next: {b.action}</>}
					{b.links.map((l, i) => (
						<span key={l.href}>
							{(b.action || i > 0) && " · "}
							<ActLink link={l} />
						</span>
					))}
				</div>
			)}
		</>
	);
}

/** A blocker's link, internal ones by the router. */
function ActLink({ link }: { link: BlockerLink }) {
	return link.external ? (
		<a href={link.href} {...external}>
			{link.text}
		</a>
	) : (
		<Link to={link.href}>{link.text}</Link>
	);
}

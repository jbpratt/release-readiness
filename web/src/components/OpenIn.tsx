import {
	Dropdown,
	DropdownItem,
	DropdownList,
	MenuToggle,
} from "@patternfly/react-core";
import { ExternalLinkAltIcon } from "@patternfly/react-icons";
import { useState } from "react";
import { Link, type To } from "react-router-dom";
import { external } from "./OperatorImages";

/**
 * The release-wide places to open, behind one toggle. An item with an
 * href opens it in a new tab, one with a to is a router link, and one with
 * neither is left out.
 */
export default function OpenIn({
	items,
}: {
	items: { text: string; href?: string | null; to?: To }[];
}) {
	const [open, setOpen] = useState(false);
	return (
		<Dropdown
			isOpen={open}
			onSelect={() => setOpen(false)}
			onOpenChange={setOpen}
			popperProps={{ position: "right" }}
			toggle={(ref) => (
				<MenuToggle
					ref={ref}
					variant="secondary"
					size="sm"
					isExpanded={open}
					onClick={() => setOpen(!open)}
				>
					Open in
				</MenuToggle>
			)}
		>
			<DropdownList>
				{items
					.filter((i) => i.href || i.to)
					.map(({ text, href, to }) =>
						href ? (
							<DropdownItem key={text} to={href} {...external}>
								{text} {/* PF's own external icon shows only on hover. */}
								<ExternalLinkAltIcon
									style={{
										marginInlineStart: "0.25rem",
										color: "var(--pf-t--global--icon--color--subtle)",
									}}
								/>
							</DropdownItem>
						) : (
							<DropdownItem
								key={text}
								component={(p) => <Link {...p} to={to ?? ""} />}
							>
								{text}
							</DropdownItem>
						),
					)}
			</DropdownList>
		</Dropdown>
	);
}

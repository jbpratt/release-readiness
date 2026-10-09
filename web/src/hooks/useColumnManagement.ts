import type { ColumnManagementModalColumn } from "@patternfly/react-component-groups";
import { useCallback, useMemo, useState } from "react";

export interface ColumnDef {
	key: string;
	label: string;
}

function loadVisibility(
	storageKey: string,
	columns: ColumnDef[],
): Record<string, boolean> {
	try {
		const raw = localStorage.getItem(storageKey);
		if (!raw) return {};
		const saved = JSON.parse(raw) as Record<string, boolean>;
		const result: Record<string, boolean> = {};
		for (const col of columns) {
			if (col.key in saved) {
				result[col.key] = saved[col.key];
			}
		}
		return result;
	} catch {
		return {};
	}
}

function saveVisibility(
	storageKey: string,
	visibility: Record<string, boolean>,
) {
	try {
		localStorage.setItem(storageKey, JSON.stringify(visibility));
	} catch {
		// localStorage may be unavailable
	}
}

export function useColumnManagement(storageKey: string, columns: ColumnDef[]) {
	const [overrides, setOverrides] = useState(() =>
		loadVisibility(storageKey, columns),
	);
	const [isModalOpen, setIsModalOpen] = useState(false);

	const isColumnVisible = useCallback(
		(key: string) => overrides[key] ?? true,
		[overrides],
	);

	const visibleColumns = useMemo(
		() => columns.filter((col) => isColumnVisible(col.key)),
		[columns, isColumnVisible],
	);

	const appliedColumns: ColumnManagementModalColumn[] = useMemo(
		() =>
			columns.map((col) => ({
				key: col.key,
				title: col.label,
				isShownByDefault: true,
				isShown: isColumnVisible(col.key),
			})),
		[columns, isColumnVisible],
	);

	const applyColumns = useCallback(
		(newColumns: ColumnManagementModalColumn[]) => {
			const newOverrides: Record<string, boolean> = {};
			for (const col of newColumns) {
				newOverrides[col.key] = col.isShown ?? true;
			}
			setOverrides(newOverrides);
			saveVisibility(storageKey, newOverrides);
		},
		[storageKey],
	);

	const openModal = useCallback(() => setIsModalOpen(true), []);
	const closeModal = useCallback(() => setIsModalOpen(false), []);

	return {
		visibleColumns,
		isColumnVisible,
		appliedColumns,
		applyColumns,
		isModalOpen,
		openModal,
		closeModal,
	};
}

import { useCallback, useEffect, useRef, useState } from "react";

interface CacheEntry<T> {
	data: T;
	timestamp: number;
}

const cache = new Map<string, CacheEntry<unknown>>();

const DEFAULT_TTL_MS = 60_000;

export function useCachedFetch<T>(
	key: string,
	fetcher: () => Promise<T>,
	ttlMs = DEFAULT_TTL_MS,
): {
	data: T | undefined;
	loading: boolean;
	error: Error | undefined;
	refetch: () => void;
} {
	const [data, setData] = useState(
		() => (cache.get(key) as CacheEntry<T> | undefined)?.data,
	);
	const [loading, setLoading] = useState(() => !cache.has(key));
	const [error, setError] = useState<Error | undefined>();
	const fetcherRef = useRef(fetcher);
	fetcherRef.current = fetcher;

	const doFetch = useCallback(() => {
		const entry = cache.get(key) as CacheEntry<T> | undefined;
		// Show cached data while refreshing (no loading flicker)
		if (entry) setData(entry.data);
		setLoading(!entry);
		if (entry && Date.now() - entry.timestamp < ttlMs) return;
		fetcherRef
			.current()
			.then((result) => {
				cache.set(key, { data: result, timestamp: Date.now() });
				setData(result);
				setError(undefined);
			})
			.catch((err) =>
				setError(err instanceof Error ? err : new Error(String(err))),
			)
			.finally(() => setLoading(false));
	}, [key, ttlMs]);

	useEffect(() => {
		doFetch();
	}, [doFetch]);

	return { data, loading, error, refetch: doFetch };
}

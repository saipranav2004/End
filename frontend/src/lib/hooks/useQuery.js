import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * Minimal data-fetching primitive: aborts superseded requests, distinguishes
 * a first load from a background refresh (so tables can dim instead of
 * collapsing into skeletons), and exposes a retry for error states.
 *
 * `fetcher` receives an AbortSignal. `deps` behaves like a useEffect array.
 */
export function useQuery(fetcher, deps = [], { enabled = true, keepPrevious = true } = {}) {
  const [state, setState] = useState({
    data: undefined,
    error: null,
    status: enabled ? 'loading' : 'idle',
  });

  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const controllerRef = useRef(null);
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      controllerRef.current?.abort();
    };
  }, []);

  const run = useCallback(
    async (mode) => {
      controllerRef.current?.abort();
      const controller = new AbortController();
      controllerRef.current = controller;

      setState((prev) => ({
        data: keepPrevious ? prev.data : undefined,
        error: null,
        status: prev.data !== undefined && mode === 'refresh' ? 'refreshing' : 'loading',
      }));

      try {
        const data = await fetcherRef.current(controller.signal);
        if (!mountedRef.current || controller.signal.aborted) return;
        setState({ data, error: null, status: 'success' });
      } catch (error) {
        if (!mountedRef.current || controller.signal.aborted || error?.code === 'CANCELLED') return;
        setState((prev) => ({
          data: keepPrevious ? prev.data : undefined,
          error,
          status: 'error',
        }));
      }
    },
    [keepPrevious],
  );

  useEffect(() => {
    if (!enabled) {
      setState({ data: undefined, error: null, status: 'idle' });
      return;
    }
    run('refresh');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, run, ...deps]);

  const refetch = useCallback(() => run('initial'), [run]);

  return {
    data: state.data,
    error: state.error,
    status: state.status,
    isLoading: state.status === 'loading',
    isRefreshing: state.status === 'refreshing',
    isError: state.status === 'error',
    isSuccess: state.status === 'success',
    refetch,
  };
}

/** Companion primitive for writes: tracks pending/err per invocation. */
export function useMutation(mutator) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(null);
  const mutatorRef = useRef(mutator);
  mutatorRef.current = mutator;

  const mutate = useCallback(async (input) => {
    setPending(true);
    setError(null);
    try {
      const result = await mutatorRef.current(input);
      return { ok: true, result };
    } catch (err) {
      setError(err);
      return { ok: false, error: err };
    } finally {
      setPending(false);
    }
  }, []);

  return { mutate, pending, error, reset: () => setError(null) };
}

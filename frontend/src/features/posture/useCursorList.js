import { useState } from 'react';
import { useQuery } from '../../lib/hooks';

/**
 * A keyset-paged list read with "load more": pages accumulate in order, each
 * read with the cursor the previous one returned. `fetchPage(cursor, signal)`
 * resolves to { nextCursor, ... }. Any change to `deps` starts again from the
 * first page; `reload()` does too, keeping the deps.
 */
export function useCursorList(fetchPage, deps) {
  const key = JSON.stringify(deps);
  const fresh = { key, cursor: null, pages: [], nonce: 0 };
  const [state, setState] = useState(fresh);
  const current = state.key === key ? state : { ...fresh, nonce: state.nonce };

  const query = useQuery(
    async (signal) => ({ ...(await fetchPage(current.cursor, signal)), key, cursor: current.cursor }),
    [key, current.cursor, current.nonce],
  );

  const latest = query.data && query.data.key === key && query.data.cursor === current.cursor ? query.data : null;
  const pages = latest ? [...current.pages, latest] : current.pages;
  const next = latest?.nextCursor ?? null;

  return {
    pages,
    next,
    query,
    loading: query.isLoading && pages.length === 0,
    loadingMore: query.isLoading && pages.length > 0,
    loadMore: () => {
      if (latest && next) setState({ ...current, cursor: next, pages: [...current.pages, latest] });
    },
    reload: () => setState({ ...fresh, nonce: current.nonce + 1 }),
  };
}

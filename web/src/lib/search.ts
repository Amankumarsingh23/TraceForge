export interface Filters {
  from: string;
  to: string;
  service: string;
  status: string;
  limit: string;
}
export function defaultFilters(): Filters {
  const to = new Date();
  return {
    from: new Date(to.getTime() - 3600000).toISOString(),
    to: to.toISOString(),
    service: "",
    status: "",
    limit: "20",
  };
}
export function searchPath(filters: Filters, cursor = "") {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters))
    if (value) query.set(key, value);
  if (cursor) query.set("cursor", cursor);
  return `/api/traces?${query}`;
}
export interface SearchState {
  filters: Filters;
  cursor: string;
  previous: string[];
}
export function applyFilters(filters: Filters): SearchState {
  return { filters, cursor: "", previous: [] };
}
export function nextPage(state: SearchState, cursor: string): SearchState {
  return { ...state, cursor, previous: [...state.previous, state.cursor] };
}
export function previousPage(state: SearchState): SearchState {
  return {
    ...state,
    cursor: state.previous.at(-1) ?? "",
    previous: state.previous.slice(0, -1),
  };
}

import { SearchExplorer } from "@/components/search-explorer";
import { defaultFilters } from "@/lib/search";
export const dynamic = "force-dynamic";
export default function Traces() {
  return <SearchExplorer initial={defaultFilters()} />;
}

import { LogsExplorer } from "@/components/logs-explorer";
import { defaultLogFilters } from "@/lib/logs";
export const dynamic = "force-dynamic";
export default function Logs() {
  return <LogsExplorer initial={defaultLogFilters()} />;
}

import { MetricsExplorer } from "@/components/metrics-explorer";
import { analyticsQuery } from "@/lib/analytics-query";
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  return <MetricsExplorer initialQuery={analyticsQuery(await searchParams)} />;
}

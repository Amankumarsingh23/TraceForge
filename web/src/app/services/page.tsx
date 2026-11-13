import { ServicesView } from "@/components/services-view";
import { analyticsQuery } from "@/lib/analytics-query";
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  return <ServicesView initialQuery={analyticsQuery(await searchParams)} />;
}

import { ServicesView } from "@/components/services-view";
import { analyticsQuery } from "@/lib/analytics-query";
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  return (
    <ServicesView
      id={(await params).id}
      initialQuery={analyticsQuery(await searchParams)}
    />
  );
}

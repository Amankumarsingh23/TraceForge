import { DetailExplorer } from "@/components/detail-explorer";
export default async function TracePage({
  params,
}: {
  params: Promise<{ traceId: string }>;
}) {
  return <DetailExplorer traceId={(await params).traceId} />;
}

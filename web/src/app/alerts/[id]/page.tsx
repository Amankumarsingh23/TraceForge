import { ReliabilityView } from "@/components/reliability-view";
export default async function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  return <ReliabilityView kind="alerts" id={(await params).id} />;
}

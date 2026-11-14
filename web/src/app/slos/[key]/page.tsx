import { ReliabilityView } from "@/components/reliability-view";
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ key: string }>;
  searchParams: Promise<{ evaluation?: string }>;
}) {
  return (
    <ReliabilityView
      kind="slos"
      id={(await params).key}
      evaluation={(await searchParams).evaluation}
    />
  );
}

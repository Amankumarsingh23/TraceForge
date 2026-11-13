import { ComparisonDetail } from "@/components/comparisons";
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ decision?: string }>;
}) {
  const { id } = await params;
  const { decision } = await searchParams;
  return (
    <ComparisonDetail key={`${id}:${decision}`} id={id} decisionId={decision} />
  );
}

import { EvaluationRun } from "@/components/evaluations";
export default async function Page({
  params,
}: {
  params: Promise<{ runId: string }>;
}) {
  const { runId } = await params;
  return <EvaluationRun key={runId} runId={runId} />;
}

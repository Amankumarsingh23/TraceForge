import { EvaluationCase } from "@/components/evaluations";
export default async function Page({
  params,
}: {
  params: Promise<{ runId: string; caseId: string }>;
}) {
  const { runId, caseId } = await params;
  return (
    <EvaluationCase key={`${runId}:${caseId}`} runId={runId} caseId={caseId} />
  );
}

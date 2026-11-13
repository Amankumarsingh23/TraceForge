import { render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Evaluations, EvaluationRun, EvaluationCase } from "./evaluations";
afterEach(() => vi.unstubAllGlobals());
it("distinguishes loading and empty success", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ runs: [], next_cursor: "" })),
  );
  render(<Evaluations />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading");
  expect(await screen.findByText("No evaluation runs yet.")).toBeVisible();
});
it("renders run coverage and frozen fixture evaluator provenance", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        Response.json(
          url.includes("/cases")
            ? { cases: [], next_cursor: "" }
            : {
                run_id: "a".repeat(32),
                dataset_version: "support-eval-v1",
                status: "completed",
                attempted: 40,
                succeeded: 35,
                failed: 5,
                scored: 35,
                configuration: {
                  mode: "fixture",
                  evaluator_version: "constrained-evidence-v1",
                },
                aggregates: {
                  recall: {
                    value: 0.5,
                    macro_numerator: 15,
                    macro_denominator: 30,
                    eligible: 35,
                    scored: 30,
                    excluded: 5,
                    execution_failures: 5,
                  },
                },
              },
        ),
      ),
    ),
  );
  render(<EvaluationRun runId={"a".repeat(32)} />);
  expect(await screen.findByText(/Attempted 40/)).toHaveTextContent("Failed 5");
  expect(screen.getByText(/30\/35 eligible scored/)).toHaveTextContent("5 N/A");
  expect(screen.getByText("fixture")).toBeVisible();
  expect(screen.getByText("0.5000")).toBeVisible();
});
it("shows unavailable as an error rather than empty data", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json(
        {
          error: {
            code: "evaluation_backend_unavailable",
            message: "evaluation_backend_unavailable",
          },
        },
        { status: 503 },
      ),
    ),
  );
  render(<Evaluations />);
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.queryByText("No evaluation runs yet.")).toBeNull();
});
it("escapes case output, preserves pending scores and links a real trace identity", async () => {
  const tid = "b".repeat(32);
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        case_id: "support-001",
        status: "succeeded",
        semantic_status: "semantic_review_pending",
        trace_id: tid,
        trace_visibility: "pending",
        case: {
          question: "<img src=x onerror=alert(1)>",
          expected_facts: [],
          reference_answer: "independent",
          review_status: "author_reviewed",
        },
        retrieved_document_ids: ["D01"],
        context_document_ids: [],
        evaluated_output: "free form",
        original_output: "free form",
        metrics: {
          groundedness: {
            value: null,
            status: "semantic_review_pending",
            numerator: 0,
            denominator: 0,
          },
        },
        judgments: [],
      }),
    ),
  );
  const { container } = render(
    <EvaluationCase runId={"a".repeat(32)} caseId="support-001" />,
  );
  expect(
    await screen.findByText("free form", { selector: "pre" }),
  ).toBeVisible();
  expect(
    screen.getByRole("link", { name: `Inspect actual trace ${tid}` }),
  ).toHaveAttribute("href", `/traces/${tid}`);
  expect(container.querySelector("img")).toBeNull();
  expect(
    screen.getAllByText("semantic_review_pending", { selector: "td" })[0],
  ).toBeVisible();
});

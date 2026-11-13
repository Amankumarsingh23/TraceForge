"""One explicitly authorized, bounded live request; never part of default checks."""

import argparse
import json

import httpx


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--allow-billable", action="store_true", required=True)
    args = parser.parse_args()
    if not args.allow_billable:
        parser.error("explicit billable opt-in required")
    with httpx.Client(base_url="http://127.0.0.1:8084", timeout=25, trust_env=False) as client:
        health = client.get("/healthz")
        health.raise_for_status()
        if health.json().get("mode") != "live":
            raise SystemExit("Live smoke requires a separately configured live-mode server")
        response = client.post(
            "/ask",
            json={
                "question": "Summarize the synthetic refund policy. Do not call tools.",
                "max_model_calls": 1,
                "max_tool_calls": 0,
                "max_steps": 1,
                "max_attempts": 1,
            },
        )
        value = response.json()
        print(json.dumps({key: value.get(key) for key in ("run_id", "trace_id", "mode", "error")}))
        response.raise_for_status()


if __name__ == "__main__":
    main()

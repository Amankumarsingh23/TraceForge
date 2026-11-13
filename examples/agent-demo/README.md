# Synthetic support agent (Session 7)

Run from this directory with Python 3.13:

```powershell
py -3.13 -m venv .venv
.\.venv\Scripts\python.exe -m pip install -r requirements.lock
.\.venv\Scripts\python.exe -m agent_demo
```

The existing Collector must listen on loopback 4317. The app binds loopback 8084; `/healthz` reports mode, `POST /ask` takes a synthetic `question` and returns answer/run/trace IDs plus actual retrieved/supplied document IDs. See the repository [README](../../README.md#session-7-agentops-execution) for request bounds, fixtures, live environment variables, smoke command, privacy and API/UI workflow.

Default mode is an explicit fixture adapter, not live inference. The real compatible adapter requires separate live opt-in and credentials. No fallback. Do not send real customer information. `python -m agent_demo.smoke --allow-billable` requires a separately configured live server and billing authorization; default tests never run it.

Tests: `.\.venv\Scripts\python.exe -m pytest -q`; lint: `.\.venv\Scripts\python.exe -m ruff check agent_demo tests`; dependency compatibility: `.\.venv\Scripts\python.exe -m pip check`. Run the application with Ctrl+C for owned exporter flush/shutdown; forced termination can lose buffered telemetry. Resolved pins are in `requirements.lock`; `.venv` and caches stay ignored. The current Starlette TestClient warns that its HTTPX compatibility bridge is deprecated; the tests pass, and production HTTPX provider requests do not use that bridge. No unrelated web/Go dependency upgrades were made.

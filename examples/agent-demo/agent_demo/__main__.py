import uvicorn

if __name__ == "__main__":
    uvicorn.run(
        "agent_demo.app:app",
        host="127.0.0.1",
        port=8084,
        limit_concurrency=8,
        timeout_keep_alive=5,
        access_log=False,
        log_level="warning",
    )

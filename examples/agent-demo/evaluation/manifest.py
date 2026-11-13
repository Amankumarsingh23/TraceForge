import copy
import re

from .dataset import checksum


def freeze(config):
    value = copy.deepcopy(config)
    for key in ("configuration_checksum", "manifest"):
        value.pop(key, None)
    # Allowlist excludes ambient environment and secrets, not just named secrets.
    allowed = {
        "provider",
        "requested_model",
        "pricing_checksum",
        "tool_data_checksum",
        "dataset_version",
        "dataset_checksum",
        "corpus_version",
        "corpus_checksum",
        "prompt_version",
        "prompt_checksum",
        "retriever_version",
        "evaluator_version",
        "evaluator_type",
        "human_review",
        "judge",
        "mode",
        "git_commit",
        "git_dirty",
        "source_checksum",
        "k",
        "metric_k",
        "context_limit",
        "concurrency",
        "random_seed",
        "output_format",
        "case_ids",
        "output_transform",
        "execution_failure",
        "trace_lookup_mode",
        "max_attempts",
        "max_model_calls",
        "max_tool_calls",
        "measurement_version",
        "pricing_version",
        "currency",
        "generation",
        "runtime",
        "scoring_definition",
    }
    if set(value) - allowed:
        raise ValueError("manifest contains unknown fields")

    def safe(item):
        if isinstance(item, dict):
            for key, v in item.items():
                if any(
                    part in key.casefold()
                    for part in ("password", "secret", "api_key", "authorization", "credential")
                ):
                    raise ValueError("secret field forbidden")
                safe(v)
        elif isinstance(item, list):
            for v in item:
                safe(v)
        elif isinstance(item, str) and (
            item.lower().startswith("bearer ") or re.search(r"https?://[^/]*@", item)
        ):
            raise ValueError("credential value forbidden")

    safe(value)
    if (
        value.get("mode") not in {"fixture", "live"}
        or not value.get("case_ids")
        or len(set(value["case_ids"])) != len(value["case_ids"])
    ):
        raise ValueError("invalid manifest identity")
    for field in ("dataset_checksum", "corpus_checksum", "source_checksum"):
        if not re.fullmatch(r"[a-f0-9]{64}", value.get(field, "")):
            raise ValueError("invalid manifest checksum")
    value["case_ids"] = sorted(value["case_ids"])
    digest = checksum(value)
    return {
        "config_id": f"support-k{value['k']}-c{value['context_limit']}",
        "version": digest[:16],
        "checksum": digest,
        "content": value,
    }

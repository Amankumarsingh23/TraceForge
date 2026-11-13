"""Application-side fixture generation. Never imports evaluation labels/rubric."""

import json
import re

CONTRACT = "CONSTRAINED_SUPPORT_V1"


def render(claims, abstained):
    if abstained:
        return "Insufficient supplied evidence; please clarify or consult synthetic support."
    return " ".join(
        f"{c['key']}: {c['value']}." + "".join(f" [{d}]" for d in c["citations"]) for c in claims
    )


def generate(question, documents):
    # Extraction reads the actual supplied text; rules are application behavior,
    # independent of the evaluation source facts, expected answers and case IDs.
    rules = [
        ("refund_window", r"refund|return|eligib", r"within (30 days) of delivery"),
        ("damage_report_window", r"damag|broken|packag", r"damage within (7 days)"),
        ("refund_processing", r"refund|process|business", r"take (five synthetic business days)"),
        ("packaging", r"damag|packag|return", r"(Keep original packaging)"),
        ("payment_methods", r"payment|card|voucher", r"documents (card and voucher) payment"),
        (
            "cancellation",
            r"cancel|processing",
            r"(processing order may be eligible for cancellation review)",
        ),
        (
            "address_review",
            r"address|shipment",
            r"(address changes require a review before shipment)",
        ),
        (
            "warranty",
            r"warranty|defect|replacement",
            r"replacement eligibility for (manufacturing defects)",
        ),
        (
            "delivery_proof",
            r"missing|package|received",
            r"(delivered status is not proof that the customer received the package)",
        ),
        (
            "tracking",
            r"track|status|delivery",
            r"(Use the order status lookup tool for the current stored status)",
        ),
        ("delay", r"delay|transit", r"(delivery delay can occur while an order is in transit)"),
        (
            "privacy",
            r"privacy|password|credentials|personal",
            r"(Do not enter passwords, access credentials, personal addresses, or payment details)",
        ),
    ]
    claims = []
    for key, query, pattern in rules:
        if not re.search(query, question, re.I):
            continue
        for document in documents:
            match = re.search(pattern, document["text"], re.I)
            if match:
                claims.append({"key": key, "value": match.group(1), "citations": [document["id"]]})
                break
    abstained = not claims
    return {
        "contract": CONTRACT,
        "claims": claims,
        "abstained": abstained,
        "answer": render(claims, abstained),
    }


def decode(content):
    try:
        value = json.loads(content)
        if value.get("contract") != CONTRACT or type(value.get("abstained")) is not bool:
            return None
        claims = value["claims"]
        if not isinstance(claims, list) or len(claims) > 16:
            return None
        for c in claims:
            if (
                set(c) != {"key", "value", "citations"}
                or not isinstance(c["key"], str)
                or not isinstance(c["value"], str)
                or len(c["value"]) > 256
                or not isinstance(c["citations"], list)
                or len(c["citations"]) > 5
                or any(not isinstance(d, str) or len(d) > 128 for d in c["citations"])
            ):
                return None
        if value["abstained"] and claims or not value["abstained"] and not claims:
            return None
        if value["answer"] != render(claims, value["abstained"]):
            return None
        return value
    except (ValueError, TypeError, KeyError, AttributeError):
        return None

"""Index retained REA responses without re-running analysis or altering originals."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parent


def unpack(response: dict) -> dict:
    if isinstance(response.get("structuredContent"), dict):
        return response["structuredContent"]
    for part in response.get("content", []):
        if part.get("type") == "text":
            try:
                value = json.loads(part["text"])
            except json.JSONDecodeError:
                continue
            if isinstance(value, dict):
                return value
    return response


def main() -> None:
    records = []
    output = ROOT / "results"
    output.mkdir(exist_ok=True)
    for path in sorted((ROOT / "evidence").glob("*.json")):
        raw = path.read_bytes()
        response = json.loads(raw)
        value = unpack(response)
        evidence_id = value.get("evidence_id")
        row = {
            "file": str(path.relative_to(ROOT)).replace("\\", "/"),
            "file_sha256": hashlib.sha256(raw).hexdigest(),
            "file_bytes": len(raw),
            "evidence_id": evidence_id,
            "status": "error" if response.get("isError") or "error" in value else "success",
        }
        if "error" in value:
            error = value["error"]
            row["error"] = {key: error.get(key) for key in ("code", "message", "retryable")}
            detail = error.get("details", {})
            diagnostics = detail.get("diagnostics", {})
            row["startup_timeout_ms"] = detail.get("timeout_ms")
            row["analysis_profile_digest"] = diagnostics.get("profile_digest")
            row["analysis_run_id"] = diagnostics.get("run_id")
        if evidence_id:
            row.update({
                "operation": value.get("operation"),
                "subject": value.get("subject"),
                "provider": value.get("provider"),
                "analysis_profile_digest": (value.get("analysis_profile") or {}).get("digest"),
                "confidence": value.get("confidence"),
                "authority": value.get("authority"),
                "locations": value.get("locations"),
                "limitations": value.get("limitations"),
                "relationship_coverage": value.get("relationship_coverage"),
            })
            normalized = value.get("normalized_result")
            if isinstance(normalized, dict):
                row["normalized_keys"] = sorted(normalized)
                row["normalized_limitations"] = normalized.get("limitations")
                row["procedure"] = normalized.get("procedure")
                value_flow = normalized.get("native_value_flow") or {}
                row["native_value_flow_summary"] = {
                    key: value_flow.get(key) for key in (
                        "available", "truncated", "omitted_operations_lower_bound",
                        "known_omitted_inputs", "known_omitted_edges", "limitations")
                }
                for field in ("callers", "callees", "incoming_references", "outgoing_references",
                              "basic_blocks", "unresolved_calls", "referenced_strings"):
                    if isinstance(normalized.get(field), list):
                        row[f"{field}_count"] = len(normalized[field])
                pseudocode = normalized.get("pseudocode")
                if isinstance(pseudocode, str):
                    row["pseudocode_warnings"] = [
                        line.strip() for line in pseudocode.splitlines() if "WARNING:" in line
                    ]
                    pseudocode_path = output / f"{path.stem}.c"
                    pseudocode_path.write_text(
                        f"/* REA {evidence_id}; source response {row['file']}; static pseudocode. */\n"
                        + pseudocode + "\n", encoding="utf-8")
                    row["pseudocode_file"] = str(pseudocode_path.relative_to(ROOT)).replace("\\", "/")
                    row["pseudocode_characters"] = len(pseudocode)
        records.append(row)
    summary = {
        "kind": "derived-response-index; does not replace original REA Evidence",
        "source_directory": "evidence",
        "generator_file_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "response_files": len(records),
        "evidence_records": sum(bool(row["evidence_id"]) for row in records),
        "function_evidence_records": sum(row.get("operation") == "analyze_function" for row in records),
        "error_responses": sum(row["status"] == "error" for row in records),
        "records": records,
    }
    (output / "SUMMARY.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n",
                                         encoding="utf-8")
    print(json.dumps({key: value for key, value in summary.items() if key != "records"},
                     ensure_ascii=True))


if __name__ == "__main__":
    main()

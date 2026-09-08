"""Offline structural checks for Mender design examples; not conformance or system tests."""
from pathlib import Path
import hashlib
import json
import re
import sys
import yaml
from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parent
checks = []

def read_json(name):
    return json.loads((ROOT / name).read_text(encoding="utf-8"))

def safe_local(name):
    path = (ROOT / name).resolve()
    if not path.is_relative_to(ROOT) or not path.is_file():
        raise ValueError(f"Invalid local artifact reference: {name}")
    return path

def walk(value):
    yield value
    if isinstance(value, dict):
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)

def check():
    for schema_name, example_name in [
        ("plugin-manifest.schema.json", "plugin-manifest.example.json"),
        ("event-envelope.schema.json", "event.example.json"),
    ]:
        schema = read_json(schema_name)
        Draft202012Validator.check_schema(schema)
        Draft202012Validator(schema, format_checker=FormatChecker()).validate(read_json(example_name))
        checks.append(f"PASS: Schema and instance — {example_name}")
    config = read_json("connection-config.schema.json")
    Draft202012Validator.check_schema(config)
    Draft202012Validator(config).validate({"credential_ref":"conn_example"})
    checks.append("PASS: Connection config schema and reference-only example")
    manifest = read_json("plugin-manifest.example.json")
    artifact = safe_local(manifest["artifact"]["file"])
    assert hashlib.sha256(artifact.read_bytes()).hexdigest() == manifest["artifact"]["sha256"]
    for name in [manifest["config_schema_file"], manifest["ui"]["schema_file"], *manifest["tool_files"]]:
        safe_local(name)
    checks.append("PASS: Confined local references and SHA-256 artifact digest")
    tool = read_json("tool.example.json")
    for key in ("input_schema", "output_schema"):
        Draft202012Validator.check_schema(tool[key])
    Draft202012Validator(tool["input_schema"]).validate({"query":"example", "limit":10})
    Draft202012Validator(tool["output_schema"]).validate({"items":[{"name":"Example", "domain":"example.test"}]})
    checks.append("PASS: Tool schemas and representative input/output examples")
    api = yaml.safe_load((ROOT / "public-api.openapi.yaml").read_text(encoding="utf-8"))
    assert api["openapi"] == "3.1.0"
    for node in walk(api):
        if isinstance(node, dict) and "$ref" in node:
            ref = node["$ref"]
            assert ref.startswith("#/"), f"Non-local reference: {ref}"
            target = api
            for part in ref[2:].split("/"):
                target = target[part.replace("~1", "/").replace("~0", "~")]
    operation_ids = set()
    for path, item in api["paths"].items():
        expected = set(re.findall(r"\{([^}]+)\}", path))
        for method, operation in item.items():
            if method not in {"get", "post", "put", "patch", "delete", "head", "options", "trace"}:
                continue
            oid = operation["operationId"]
            assert oid not in operation_ids, f"Duplicate operationId: {oid}"
            operation_ids.add(oid)
            params = item.get("parameters", []) + operation.get("parameters", [])
            actual = {p["name"] for p in params if p["in"] == "path" and p.get("required") is True}
            assert expected == actual, (path, expected, actual)
            assert operation.get("responses"), oid
    for schema in api["components"]["schemas"].values():
        Draft202012Validator.check_schema(schema)
    checks.append(f"PASS: OpenAPI structural checks — {len(api['paths'])} paths / {len(operation_ids)} operations, unique IDs, path parameters, local references and JSON Schemas")
    checks.append("LIMIT: Not a full OpenAPI validator, MCP conformance test, security test, or running-service integration test.")
    print("\n".join(checks))

if __name__ == "__main__":
    try:
        check()
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        sys.exit(1)

#!/usr/bin/env python3
"""Validate plugin.toml and schema/*.json against the plugin contract.

Run with `make validate`. Needs only the Python standard library.
Checks:
- plugin.toml parses and carries the expected name/version/kind/entry/
  protocol/commands/permissions/limits/license/source.
- Every schema/*.json file parses as JSON and declares a $schema,
  a title, and the properties its name implies.
"""

import json
import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

failures = []


def check(cond, msg):
    if cond:
        print(f"ok: {msg}")
    else:
        failures.append(msg)
        print(f"FAIL: {msg}")


def main():
    plugin = tomllib.loads((ROOT / "plugin.toml").read_text())
    check(plugin.get("name") == "logreduce", "plugin.toml name is logreduce")
    check(plugin.get("version") == "0.1.0", "plugin.toml version is 0.1.0")
    check(plugin.get("kind") == "cli", "plugin.toml kind is cli")
    check(plugin.get("entry") == "bin/logreduce", "plugin.toml entry is bin/logreduce")
    check(plugin.get("protocol") == "arabica-plugin-v1", "plugin.toml protocol is arabica-plugin-v1")
    check(
        plugin.get("commands") == ["reduce", "self-test", "doctor", "version"],
        "plugin.toml commands are reduce, self-test, doctor, version",
    )
    perms = plugin.get("permissions", {})
    check(perms.get("read") == ["--log"], "plugin.toml permissions.read is [--log]")
    check(perms.get("write") == ["--out"], "plugin.toml permissions.write is [--out]")
    check(perms.get("network") is False, "plugin.toml permissions.network is false")
    limits = plugin.get("limits", {})
    check(limits.get("input_mb") == 200, "plugin.toml limits.input_mb is 200")
    check(limits.get("timeout_s") == 60, "plugin.toml limits.timeout_s is 60")
    check(plugin.get("license") == "MIT", "plugin.toml license is MIT")
    check(
        plugin.get("source") == "https://github.com/daxpublicfirst-arch/logreduce",
        "plugin.toml source is the real repo URL",
    )
    example = plugin.get("example", "")
    check("logreduce reduce --kind vitest" in example, "plugin.toml example names the reduce command")

    for name in ("request", "response", "error", "telemetry"):
        path = ROOT / "schema" / f"{name}.json"
        try:
            schema = json.loads(path.read_text())
        except Exception as exc:  # noqa: BLE001 - report and continue
            check(False, f"schema/{name}.json parses ({exc})")
            continue
        check(isinstance(schema, dict), f"schema/{name}.json is an object")
        check("$schema" in schema, f"schema/{name}.json declares $schema")
        check("title" in schema, f"schema/{name}.json has a title")
        props = schema.get("properties", {})
        check(bool(props), f"schema/{name}.json declares properties")

    response = json.loads((ROOT / "schema" / "response.json").read_text())
    check("findings" in response["properties"], "response schema has findings")
    check("summary" in response["properties"], "response schema has summary")
    error = json.loads((ROOT / "schema" / "error.json").read_text())
    codes = error["properties"]["error"]["properties"]["code"]["enum"]
    check("TIMEOUT" in codes, "error schema lists TIMEOUT")
    check("INPUT_MISSING" in codes, "error schema lists INPUT_MISSING")

    if failures:
        print(f"\nvalidate: {len(failures)} failure(s)")
        return 1
    print("\nvalidate: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())

"""Offline metadata fixture. No network requests or Panora imports."""
import json
import sys

request = json.load(sys.stdin)
assert request["jsonrpc"] == "2.0"
params = request["params"]
response = {"jsonrpc": "2.0", "id": request["id"]}
method = request["method"]
content = {
    "media_kind": "image",
    "kind": "https",
    "url": "https://images.example.org/mountain-123.png",
    "mime_type": "image/png",
}
attribution = {
    "title": "Mountain lake",
    "page_url": "https://example.org/images/mountain-123",
    "author": "Alex",
    "author_url": "https://example.org/alex",
    "license_url": "https://example.org/license",
}

if params.get("protocol") != 2:
    response["error"] = {
        "code": 1000, "message": "Expected Panora protocol 2.",
        "data": {"kind": "unsupported"},
    }
elif method == "describe":
    response["result"] = {
        "protocol": 2, "id": "remote", "name": "Offline remote fixture",
        "media_kinds": ["image"], "content_kinds": ["https"],
        "operations": ["describe", "validate", "discover", "resolve"],
        "fields": [], "filter_hints": ["min_width", "min_height", "ratio"],
    }
elif method == "validate":
    response["result"] = {}
elif method == "discover":
    if params["config"].get("rate_limited"):
        response["error"] = {
            "code": 1000, "message": "Try later.",
            "data": {"kind": "rate_limited", "retry_after_seconds": 120},
        }
    else:
        # Echo requested hints in an opaque reference to check cross-language values.
        response["result"] = {
            "candidates": [{
                "id": "mountain-123", "media_kind": "image",
                "ref": {"key": "mountain-123", "filter": params.get("filter")},
                "content": content, "attribution": attribution,
            }],
            "next_cursor": None,
        }
elif method == "resolve":
    response["result"] = {
        "content": content, "attribution": attribution,
        "dimensions": {"width": 5120, "height": 1440},
    }
else:
    response["error"] = {"code": -32601, "message": "Method not found"}

print(json.dumps(response))

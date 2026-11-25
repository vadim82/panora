"""Contract fixture: offer root/fixture.png, without importing any Panora code."""
import json
from pathlib import Path
import sys

request = json.loads(sys.stdin.readline())
response = {"jsonrpc": "2.0", "id": request["id"]}
params = request["params"]
operation = request["method"]

if request.get("jsonrpc") != "2.0" or params.get("protocol") != 2:
    response["error"] = {"code": 1000, "message": "Expected Panora protocol 2.", "data": {"kind": "unsupported"}}
elif operation == "describe":
    response["result"] = {
        "protocol": 2,
        "id": "local",
        "name": "Python contract fixture",
        "media_kinds": ["image"],
        "content_kinds": ["local"],
        "operations": ["describe", "validate", "discover", "resolve"],
        "fields": [],
    }
elif operation == "validate":
    if Path(params["config"]["root"]).is_dir():
        response["result"] = {}
    else:
        response["error"] = {"code": 1000, "message": "Choose a folder.", "data": {"kind": "invalid_config"}}
elif operation == "discover":
    path = str(Path(params["config"]["root"]) / "fixture.png")
    response["result"] = {
        "candidates": [{
            "id": "fixture.png",
            "media_kind": "image",
            "ref": {"path": path},
            "content": {"media_kind": "image", "kind": "local", "path": path},
        }],
        "next_cursor": None,
    }
elif operation == "resolve":
    response["result"] = {
        "content": {"media_kind": "image", "kind": "local", "path": params["ref"]["path"]}
    }
else:
    response["error"] = {"code": -32601, "message": "Method not found"}

print(json.dumps(response))

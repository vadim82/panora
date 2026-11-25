#!/usr/bin/env python3
"""Panora protocol-2 fixture for the two provider editor examples."""

import json
import os
import sys
from pathlib import Path

PROVIDER = sys.argv[1]
TAGS = ("alpine", "arch", "cloud", "forest", "lake", "mountain", "winter")
EXTENSIONS = {".jpg", ".jpeg", ".png", ".webp"}


def configure(config):
    root = Path(config.get("root", ""))
    if not root.is_absolute() or not root.is_dir():
        raise ValueError("Enter an absolute path to a readable image folder.")
    if config.get("album") == "featured" and not config.get("caption", "").strip():
        raise ValueError("Enter a caption for the featured album.")
    return root


def run(method, params):
    if params.pop("protocol") != 2:
        raise ValueError("Expected provider protocol 2.")
    if method == "describe":
        return {
            "protocol": 2,
            "id": PROVIDER,
            "name": "Fixture images",
            "media_kinds": ["image"],
            "content_kinds": ["local"],
            "operations": ["describe", "validate", "discover", "resolve", "suggest"],
            "fields": [],
        }
    if method == "suggest":
        if params["field"] != "tags":
            raise ValueError("This setting does not offer suggestions.")
        prefix = params["prefix"].casefold()
        return {"values": [tag for tag in TAGS if tag.startswith(prefix)][:params["limit"]]}

    root = configure(params["config"])
    if method == "validate":
        return {}
    if method == "discover":
        offset = params["cursor"] or 0
        limit = params["limit"]
        images = []
        with os.scandir(root) as entries:
            for i, path in enumerate(entries):
                if i < offset or not path.is_file(follow_symlinks=False) or Path(path.name).suffix.lower() not in EXTENSIONS:
                    continue
                if len(images) == limit:
                    return {"candidates": images, "next_cursor": i}
                images.append({"id": path.name, "media_kind": "image", "ref": path.name,
                               "content": {"kind": "local", "media_kind": "image", "path": path.path}})
        return {"candidates": images, "next_cursor": None}
    if method == "resolve":
        name = params["id"]
        path = root / name
        if name != Path(name).name or params["ref"] != name or path.is_symlink() or not path.is_file():
            raise ValueError("The image is no longer in this folder.")
        return {"content": {"kind": "local", "media_kind": "image", "path": str(path)}}
    raise ValueError("Unknown method.")


def main():
    request = json.loads(sys.stdin.readline(65536))
    response = {"jsonrpc": "2.0", "id": request["id"]}
    try:
        response["result"] = run(request["method"], request["params"])
    except (OSError, TypeError, KeyError):
        response["error"] = {"code": -32603, "message": "Fixture provider failed."}
    except ValueError as err:
        response["error"] = {"code": 1000, "message": str(err), "data": {"kind": "invalid_config"}}
    print(json.dumps(response))


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Forward MCP stdio messages to an already running local Pudding daemon."""

import argparse
import http.client
import ipaddress
import json
import os
from pathlib import Path
import sys
from urllib.parse import urlsplit


def write_message(message):
    data = json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    sys.stdout.buffer.write(data + b"\n")
    sys.stdout.buffer.flush()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--home", default=os.environ.get("PUDDING_HOME") or "~/.pudding-dev",
        help="daemon data home (default: PUDDING_HOME or ~/.pudding-dev)",
    )
    parser.add_argument(
        "--addr", default="127.0.0.1:9679",
        help="local daemon host:port or http://host:port (default: 127.0.0.1:9679)",
    )
    parser.add_argument("--runtime-id", help="explicit Desktop runtime; omitted for automatic selection")
    args = parser.parse_args()

    try:
        address = urlsplit(args.addr if "://" in args.addr else "http://" + args.addr)
        host, port = address.hostname, address.port
        local = host == "localhost" or (host and ipaddress.ip_address(host).is_loopback)
        if (address.scheme != "http" or not local or not port or address.path not in ("", "/")
                or address.query or address.fragment or address.username is not None):
            raise ValueError("--addr must be a loopback HTTP address, such as http://127.0.0.1:9679")
    except (OSError, ValueError) as error:
        print(f"pudding-mcp: {error}", file=sys.stderr)
        return 1

    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    if args.runtime_id:
        headers["X-Pudding-Runtime-ID"] = args.runtime_id

    for line in sys.stdin.buffer:
        if not line.strip():
            continue
        request = None
        connection = http.client.HTTPConnection(host, port, timeout=660)
        try:
            request = json.loads(line)
            # The daemon may restart with a new token while Codex keeps this process.
            token = (Path(args.home).expanduser() / "daemon.token").read_text(encoding="utf-8").strip()
            if not token:
                raise ValueError("daemon.token is empty")
            headers["Authorization"] = "Bearer " + token
            connection.request("POST", "/mcp", body=line, headers=headers)
            response = connection.getresponse()
            body = response.read()
            if response.status == 202:
                continue
            if response.status != 200:
                if response.getheader("Content-Type", "").split(";", 1)[0] == "application/json":
                    message = json.loads(body)
                    if isinstance(message, dict) and message.get("jsonrpc") == "2.0" and "error" in message:
                        write_message(message)
                        continue
                raise RuntimeError(f"daemon returned HTTP {response.status} {response.reason}")
            message = json.loads(body)
            if isinstance(request, dict) and request.get("method") == "initialize" and "result" in message:
                headers["MCP-Protocol-Version"] = message["result"]["protocolVersion"]
            write_message(message)
        except (OSError, ValueError, http.client.HTTPException, RuntimeError) as error:
            print(f"pudding-mcp: {error}", file=sys.stderr)
            if isinstance(request, dict) and "id" in request:
                write_message({
                    "jsonrpc": "2.0", "id": request["id"],
                    "error": {"code": -32000, "message": str(error)},
                })
            # Fail only this request; never replay a call that may have executed.
            continue
        finally:
            connection.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())

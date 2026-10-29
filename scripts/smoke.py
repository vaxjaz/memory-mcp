#!/usr/bin/env python3
"""Exercise an actual packaged stdio executable without a model or MCP client app."""
import json
from pathlib import Path
import queue
import subprocess
import sys
import tempfile
import threading


class Host:
    def __init__(self, binary, data_dir):
        self.p = subprocess.Popen([binary, "serve", "--data-dir", data_dir],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True)
        self.lines = queue.Queue()
        self.errors = []
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()
        self.err_reader = threading.Thread(target=lambda: self.errors.extend(self.p.stderr.readlines()), daemon=True)
        self.err_reader.start()
        self.counter = 0
        self.request("initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                     "clientInfo": {"name": "generic-offline-test", "version": "1"}})
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def _read(self):
        for line in self.p.stdout:
            self.lines.put(line)
        self.lines.put(None)

    def send(self, value):
        self.p.stdin.write(json.dumps(value, ensure_ascii=False) + "\n")
        self.p.stdin.flush()

    def request(self, method, params):
        self.counter += 1
        self.send({"jsonrpc": "2.0", "id": self.counter, "method": method, "params": params})
        while True:
            line = self.lines.get(timeout=20)
            assert line is not None, "Server closed stdout"
            response = json.loads(line)  # Fails on non-protocol stdout logging.
            if response.get("id") == self.counter:
                assert "error" not in response, response
                return response["result"]

    def tool(self, name, args, expect_error=False):
        result = self.request("tools/call", {"name": name, "arguments": args})
        assert bool(result.get("isError", False)) == expect_error, result
        if expect_error:
            return result
        # Verify the text fallback expected by clients predating structuredContent.
        return json.loads(result["content"][0]["text"])

    def close(self):
        self.p.stdin.close()
        try:
            code = self.p.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.p.kill()
            self.p.wait()
            raise
        self.reader.join(timeout=1)
        self.err_reader.join(timeout=1)
        self.p.stdout.close()
        self.p.stderr.close()
        assert code == 0, "".join(self.errors)


def main():
    binary = str(Path(sys.argv[1]).resolve())
    with tempfile.TemporaryDirectory(prefix="memory-mcp-smoke-") as temp:
        data = str(Path(temp) / "local-data")
        host = Host(binary, data)
        try:
            assert len(host.request("tools/list", {})["tools"]) == 5
            assert host.tool("memory_bootstrap", {})["integration_protocol"] == "1"
            draft = {"kind": "record", "scope": "project:smoke", "title": "保存反馈",
                     "body": "方案 B 更清楚，只评价当前表单。", "origin": "host_quote"}
            original = host.tool("memory_record", draft)
            hypothesis = {"kind": "hypothesis", "scope": draft["scope"], "title": "局部假设",
                          "body": "当前表单可能偏好明确保存反馈，其他场景未知。", "origin": "model_inference",
                          "links": [{"target_id": original["id"], "relation": "supports"}]}
            inferred = host.tool("memory_revise", {"draft": hypothesis})
            assert len(host.tool("memory_search", {"scope": draft["scope"], "query": "保存"})["results"]) == 2
            corrected = dict(draft, body="纠正：只偏好方案 B 的保存状态，不包括它的布局。")
            current = host.tool("memory_revise", {"previous_id": original["id"], "draft": corrected})
            assert host.tool("memory_read", {"id": inferred["id"]})["entry"]["needs_review"]
            host.tool("memory_revise", {"previous_id": original["id"], "draft": corrected}, expect_error=True)
        finally:
            host.close()

        # Reopen from a different host process to verify persistence.
        host = Host(binary, data)
        try:
            old = host.tool("memory_read", {"id": original["id"]})
            assert old["entry"]["body"] == draft["body"]
            assert old["current_id"] == current["id"]
        finally:
            host.close()

        exported = str(Path(temp) / "export.json")
        subprocess.run([binary, "export", "--data-dir", data, "--out", exported], check=True, capture_output=True)
        assert len(json.loads(Path(exported).read_text())["entries"]) == 3
        subprocess.run([binary, "forget", "--data-dir", data, "--id", original["id"], "--yes"], check=True, capture_output=True)
        status = json.loads(subprocess.check_output([binary, "status", "--data-dir", data], text=True))
        assert status["entries_including_history"] == 0
        config = json.loads(subprocess.check_output([binary, "config"], text=True))
        assert config["mcpServers"]["decision-memory"]["command"] == binary
    print("PASS: legacy MCP stdio, 5 tools, revisions, stale evidence, persistence, export, forget, config")


if __name__ == "__main__":
    main()

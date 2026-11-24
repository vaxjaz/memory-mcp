#!/usr/bin/env python3
"""Check the capability matrix against its own rules.

Every row states one of the four verdicts and carries evidence; the per-module
counts and the total in the overview equal what the rows say. A promise without
a verdict, or a summary that disagrees with the rows, fails.
"""
import argparse
import json
import re
import sys
from pathlib import Path

VERDICTS = ["✅", "⚠️", "❌", "❓"]
ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--document", type=Path, default=ROOT / "docs" / "CAPABILITY_MATRIX.md")
    args = parser.parse_args()
    lines = args.document.read_text(encoding="utf-8").splitlines()
    problems, per, module, titles = [], {}, None, {}
    for line in lines:
        if line.startswith("## "):
            module = None
            found = re.match(r"(\d+)\. (.+)", line[3:])
            if found:
                module = int(found.group(1))
                titles[found.group(2).strip()] = module
        if not line.startswith("| ") or module is None:
            continue
        cells = [c.strip() for c in line.split("|")[1:-1]]
        if len(cells) != 3 or cells[1] == "状态" or set(cells[1]) <= {"-"}:
            continue
        if cells[1] not in VERDICTS:
            problems.append("%s: verdict %r is not one of the four" % (cells[0], cells[1]))
            continue
        if not cells[2]:
            problems.append("%s: no evidence" % cells[0])
        counts = per.setdefault(module, dict.fromkeys(VERDICTS, 0))
        counts[cells[1]] += 1
    # The overview names each module by the title of its section.
    rows = [line for line in lines if re.match(r"\| .* \| \d+ \| \d+ \| \d+ \| \d+ \|", line)]
    if len(rows) < 2:
        problems.append("the overview table was not found")
    else:
        listed = {}
        for line in rows:
            cells = [c.strip() for c in line.split("|")[1:-1]]
            if cells[0] == "合计":
                continue
            number = titles.get(cells[0])
            if number is None:
                problems.append("overview row that names no module section: %s" % cells[0])
                continue
            listed[number] = [int(c) for c in cells[1:5]]
        for number in sorted(set(listed) | set(per)):
            want = [per.get(number, {}).get(v, 0) for v in VERDICTS]
            if listed.get(number) != want:
                problems.append("module %s: the overview says %s, the rows say %s" % (number, listed.get(number), want))
        total = [c.strip() for c in rows[-1].split("|")[1:-1]]
        summed = [sum(v[i] for v in listed.values()) for i in range(4)]
        recounted = [sum(counts[verdict] for counts in per.values()) for verdict in VERDICTS]
        if total[0] != "合计":
            problems.append("the last row of the overview is not the total")
        elif [int(c) for c in total[1:5]] != summed or summed != recounted:
            problems.append("the total row says %s, the module rows sum to %s, the rows recount to %s" % (total[1:5], summed, recounted))
    result = dict(result="PASS" if not problems else "FAIL", document=str(args.document),
                  cells=sum(sum(c.values()) for c in per.values()),
                  by_verdict={v: sum(c[v] for c in per.values()) for v in VERDICTS}, problems=problems)
    print(json.dumps(result, ensure_ascii=False))
    return 1 if problems else 0


if __name__ == "__main__":
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8")
    sys.exit(main())

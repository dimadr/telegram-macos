import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


name, *command = sys.argv[1:]
logs = Path(".mieru-work/logs")
logs.mkdir(parents=True, exist_ok=True)
started = time.time()
build_minutes = int(os.environ.get("MIERU_CI_BUILD_MINUTES", "115"))
if not 1 <= build_minutes <= 115:
    raise ValueError("MIERU_CI_BUILD_MINUTES must be between 1 and 115")
deadline = int(os.environ["MIERU_CI_STARTED_AT"]) + build_minutes * 60
remaining = deadline - started
phase_minutes = int(os.environ.get("MIERU_CI_PHASE_MINUTES", str(build_minutes)))
if not 1 <= phase_minutes <= build_minutes:
    raise ValueError("MIERU_CI_PHASE_MINUTES exceeds the build deadline")
remaining = min(remaining, phase_minutes * 60)
status = 124
log = logs / (name + ".log")
with log.open("wb") as output:
    if remaining <= 0:
        output.write(b"The CI build budget is exhausted; remaining job time is reserved for artifacts.\n")
    else:
        process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            status = process.wait(timeout=remaining)
        except subprocess.TimeoutExpired:
            if name == "package-graph":
                try:
                    subprocess.run(["sample", str(process.pid), "5", "-file", str(logs / "package-graph-sample.txt")], stdout=output, stderr=subprocess.STDOUT, timeout=15, check=False)
                except (OSError, subprocess.TimeoutExpired) as error:
                    output.write(("Stack sampling failed: " + str(error) + "\n").encode())
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            output.write(b"\nCI phase/build deadline reached; stopped to preserve logs and artifacts.\n")

record = {"phase": name, "exit_code": status, "seconds": round(time.time() - started, 2)}
with (logs / "results.jsonl").open("a") as output:
    output.write(json.dumps(record) + "\n")
print(json.dumps(record), flush=True)
if status != 0:
    candidates = [path for path in logs.glob("*.log") if path.stat().st_size]
    if candidates:
        latest = max(candidates, key=lambda path: path.stat().st_mtime)
        print("Diagnostic tail: " + str(latest), flush=True)
        print("\n".join(latest.read_text(errors="replace").splitlines()[-40:]), flush=True)
sys.exit(status)

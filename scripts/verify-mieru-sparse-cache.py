import hashlib
import json
from pathlib import Path
import subprocess
import tempfile


with tempfile.TemporaryDirectory(dir=".mieru-work/DerivedData/CompilationCache.noindex") as temporary:
    root = Path(temporary).resolve()
    source = root / "input"
    source.write_bytes(bytes(16 * 1024 * 1024))
    with source.open("r+b") as output:
        output.seek(4096)
        output.write(b"mieru-sparse-cache")
    archive = root / "cache.tar"
    restored = root / "restored"
    restored.mkdir()
    subprocess.run(["/usr/bin/tar", "--posix", "-cf", str(archive), "-C", str(root), "input"], check=True)
    subprocess.run(["bash", "scripts/mieru-cache-tar.sh", "-xf", str(archive), "-C", str(restored), "--delay-directory-restore"], check=True)
    actual = restored / "input"
    if hashlib.sha256(source.read_bytes()).digest() != hashlib.sha256(actual.read_bytes()).digest():
        raise RuntimeError("Sparse cache extraction changed file contents")
    stat = actual.stat()
    allocated = stat.st_blocks * 512
    report = {"logical_bytes": stat.st_size, "allocated_bytes": allocated}
    Path(".mieru-work/logs/sparse-cache-test.json").write_text(json.dumps(report) + "\n")
    print(json.dumps(report), flush=True)
    if allocated >= stat.st_size // 4:
        raise RuntimeError("Sparse cache extraction did not preserve disk space")

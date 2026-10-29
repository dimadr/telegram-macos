from pathlib import Path
import shutil


root = Path(".mieru-work/DerivedData").resolve()
for relative in ["Build/Intermediates.noindex/XCBuildData/PIFCache", "SourcePackages/workspace-state.json"]:
    target = root / relative
    if not target.resolve().is_relative_to(root):
        raise RuntimeError("Package graph cache is outside DerivedData")
    if target.is_dir():
        shutil.rmtree(target)
    else:
        target.unlink(missing_ok=True)
    print("Reset package graph metadata: " + relative, flush=True)

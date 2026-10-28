from pathlib import Path
import re
import subprocess


logs = Path(".mieru-work/logs")
logs.mkdir(parents=True, exist_ok=True)
objects = list(Path(".mieru-work/DerivedData/Build/Intermediates.noindex").rglob("SqliteValueBox.o"))
resolved = False
with (logs / "sqlite-crash-symbolication.log").open("w") as output:
    output.write("Reported image UUID: 2E6F58CB-E0E0-3B96-81C2-C34D80005558\nReported function offset: 12792 (0x31f8)\n")
    for object_file in objects:
        symbols = subprocess.check_output(["xcrun", "nm", "-n", str(object_file)], text=True)
        for line in symbols.splitlines():
            match = re.match(r"^([0-9a-fA-F]+)\s+[tT]\s+(\S+)", line)
            if not match or "openDatabase" not in match[2] or "SqliteValueBox" not in match[2]:
                continue
            name = subprocess.check_output(["xcrun", "swift-demangle", "--compact", match[2]], text=True)
            if "closure" in name or "default argument" in name:
                continue
            address = int(match[1], 16) + 12792
            output.write(f"\nObject: {object_file}\nFunction: {name}Object address: {address:#x}\n")
            lookup = subprocess.run(["xcrun", "dwarfdump", "--lookup", hex(address), str(object_file)], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            output.write(lookup.stdout)
            output.flush()
            subprocess.run(["xcrun", "llvm-objdump", "--disassemble", f"--start-address={address-96}", f"--stop-address={address+32}", str(object_file)], stdout=output, stderr=subprocess.STDOUT, check=False)
            resolved = resolved or "Line info:" in lookup.stdout
if not objects:
    raise RuntimeError("Cached SqliteValueBox.o is unavailable; original crash cannot be source-symbolicated")
if not resolved:
    raise RuntimeError("Cached object did not resolve the reported offset to a source line; see symbolication log")
print("SQLite crash offset source lookup completed; verify object/source provenance before assigning cause")

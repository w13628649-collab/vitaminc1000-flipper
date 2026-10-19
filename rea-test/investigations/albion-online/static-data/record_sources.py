"""Record source/script hashes and exact local build identity without modifying inputs."""
import datetime
import hashlib
import json
from pathlib import Path

base = Path(__file__).resolve().parent
game = Path("C:/Users/ludy/AppData/Local/AlbionOnline/game")
records = []
for p in sorted((base / "sources").iterdir()):
    if p.is_file():
        records.append({"path": p.relative_to(base).as_posix(), "bytes": p.stat().st_size,
                        "sha256": hashlib.sha256(p.read_bytes()).hexdigest()})
scripts = [{"path": p.name, "bytes": p.stat().st_size,
            "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in sorted(base.glob("*.py"))]
identity = [{"path": str(p), "bytes": p.stat().st_size,
             "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
            for p in (game / "version.txt", game / "GameAssembly.dll",
                      game / "Albion-Online_Data/il2cpp_data/Metadata/global-metadata.dat")]
out = {"recorded_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
       "target_root": "C:/Users/ludy/AppData/Local/AlbionOnline",
       "local_client_build": (game / "version.txt").read_text().strip(),
       "dumper_repository": "https://github.com/ao-data/albiondata-bin-dumper",
       "dumper_commit": "e73a181eafd0db785f889aba8dbe7c69f83b71ad",
       "assetstudio_repository": "https://github.com/Perfare/AssetStudio",
       "assetstudio_commit": "d158e864b556b5970709c2a52e47944d53aa98a2",
       "historical_asia_patch": "https://raw.githubusercontent.com/ao-data/ao-bin-dumps/1a469e53de028c5f85cdd80505f10cbd0a2515cf/cluster/world_asia_patch.xml",
       "historical_asia_patch_applied": False,
       "dependency": {"package": "pycryptodome", "version": "3.23.0", "scope": str(base / "dependencies")},
       "sources": records, "scripts": scripts, "identity": identity,
       "evidence_class": "local static format parsing plus public decoder source; no REA Evidence IDs"}
(base / "source-manifest.json").write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
print(json.dumps({"sources": len(records), "scripts": len(scripts), "identity_files": len(identity)}))

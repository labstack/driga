#!/usr/bin/env python3
"""Add release metadata and the committed Murmur icon to a built Driga.app bundle."""

import plistlib
import re
import shutil
import sys
from pathlib import Path


def assemble(app, version):
    contents = app / "Contents"
    if not (contents / "MacOS/driga").is_file():
        raise ValueError("bundle executable is missing")
    # CFBundleVersion is numeric; the user-facing version retains the alpha or
    # snapshot suffix. CalVer's three numeric components come from the tag.
    match = re.fullmatch(r"(\d+\.\d+\.\d+)(?:-[A-Za-z0-9.-]+)?", version)
    if not match:
        raise ValueError("expected a three-component release version")
    resources = contents / "Resources"
    resources.mkdir(parents=True, exist_ok=True)
    icon = Path(__file__).resolve().parents[1] / "internal/web/static/brand/Driga.icns"
    shutil.copyfile(icon, resources / icon.name)
    info = {
        "CFBundleIdentifier": "run.driga",
        "CFBundleName": "Driga",
        "CFBundleDisplayName": "Driga",
        "CFBundleExecutable": "driga",
        "CFBundlePackageType": "APPL",
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleShortVersionString": match[1],
        "CFBundleVersion": match[1],
        "DrigaReleaseVersion": version,
        "CFBundleIconFile": "Driga.icns",
        # Finder, Dock, Spotlight and login-item launches reach app mode only
        # through this variable; the app unsets it after reading. There is no
        # LSUIElement: the app picks its activation policy at run time.
        "LSEnvironment": {"DRIGA_APP": "1"},
        "NSHighResolutionCapable": True,
    }
    with (contents / "Info.plist").open("wb") as output:
        plistlib.dump(info, output, sort_keys=True)


if __name__ == "__main__":
    assemble(Path(sys.argv[1]), sys.argv[2])

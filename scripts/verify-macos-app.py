#!/usr/bin/env python3
"""Verify a Driga Darwin archive without executing it or extracting unsafe paths."""

import plistlib
import re
import struct
import sys
import tarfile
import zlib
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def verify_icns(data):
    require(data[:4] == b"icns" and struct.unpack(">I", data[4:8])[0] == len(data), "invalid ICNS header")
    expected = {"icp4": 16, "ic11": 32, "icp5": 32, "ic12": 64,
                "ic07": 128, "ic13": 256, "ic08": 256, "ic14": 512,
                "ic09": 512, "ic10": 1024}
    offset, seen, legacy = 8, set(), {}
    while offset < len(data):
        kind = data[offset:offset + 4].decode("ascii")
        length = struct.unpack(">I", data[offset + 4:offset + 8])[0]
        require(length >= 8 and offset + length <= len(data), "invalid ICNS chunk length")
        png = data[offset + 8:offset + length]
        # iconutil may encode older small sizes using legacy ICNS chunks.
        if kind in ("is32", "il32", "s8mk", "l8mk"):
            legacy[kind] = png
        if kind in expected:
            require(kind not in seen, "duplicate icon representation")
            require(png[:8] == b"\x89PNG\r\n\x1a\n", "expected PNG-backed ICNS representation")
            require(struct.unpack(">II", png[16:24]) == (expected[kind],) * 2, "incorrect icon dimensions")
            pos, pixels = 8, b""
            while pos < len(png):
                size = struct.unpack(">I", png[pos:pos + 4])[0]
                chunk = png[pos + 4:pos + 8 + size]
                require(zlib.crc32(chunk) == struct.unpack(">I", png[pos + 8 + size:pos + 12 + size])[0], "invalid PNG CRC")
                if chunk[:4] == b"IDAT": pixels += chunk[4:]
                pos += size + 12
            require(zlib.decompress(pixels), "empty PNG")
            seen.add(kind)
        offset += length
    for representation, rgb, mask, size in (("icp4", "is32", "s8mk", 16), ("icp5", "il32", "l8mk", 32)):
        if representation not in seen and legacy.get(rgb) and len(legacy.get(mask, b"")) == size * size:
            seen.add(representation)
    require(offset == len(data) and seen == set(expected), "incomplete ICNS size set")


def verify_archive(path, version):
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        names = [entry.name for entry in members if not entry.isdir()]
        require(len(names) == len(set(names)), "duplicate archive members")
        require(all(entry.isfile() or entry.isdir() for entry in members), "unexpected archive link or special file")
        required = {"LICENSE", "NOTICE", "README.md", "third-party/fonts-LICENSE.txt",
                    "third-party/vendor-LICENSES.txt", "Driga.app/Contents/Info.plist",
                    "Driga.app/Contents/MacOS/driga", "Driga.app/Contents/Resources/Driga.icns"}
        # CodeResources is Apple's app staple; the resource seal is separate.
        allowed = required | {"Driga.app/Contents/_CodeSignature/CodeResources", "Driga.app/Contents/CodeResources"}
        require(required <= set(names) <= allowed, "unexpected contents in Darwin archive")
        directories = {str(parent) for name in allowed for parent in Path(name).parents if str(parent) != "."}
        require(all(entry.name.rstrip("/") in directories for entry in members if entry.isdir()), "unexpected archive directory")
        def read(name):
            return archive.extractfile(name).read()
        info = plistlib.loads(read("Driga.app/Contents/Info.plist"))
        numeric = re.match(r"^\d+\.\d+\.\d+", version)[0]
        for key, value in {"CFBundleIdentifier": "run.driga", "CFBundleName": "Driga",
                           "CFBundleDisplayName": "Driga", "CFBundleExecutable": "driga",
                           "CFBundlePackageType": "APPL", "CFBundleInfoDictionaryVersion": "6.0",
                           "CFBundleShortVersionString": numeric, "CFBundleVersion": numeric,
                           "DrigaReleaseVersion": version, "CFBundleIconFile": "Driga.icns",
                           "LSEnvironment": {"DRIGA_APP": "1"},
                           "NSHighResolutionCapable": True}.items():
            require(info.get(key) == value, f"incorrect {key} in Info.plist")
        require("LSUIElement" not in info, "Info.plist must not set LSUIElement")
        binary = archive.getmember("Driga.app/Contents/MacOS/driga")
        require(binary.mode & 0o111 and binary.size > 0, "bundle executable is empty or not executable")
        verify_icns(read("Driga.app/Contents/Resources/Driga.icns"))


if __name__ == "__main__":
    try:
        verify_archive(Path(sys.argv[1]), sys.argv[2])
    except (ValueError, KeyError, OSError, struct.error, zlib.error) as error:
        sys.exit(f"invalid macOS app archive: {error}")

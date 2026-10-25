#!/usr/bin/env python3
"""Generate brand variants from murmur.svg and wordmark.svg.

Run from any directory: python3 scripts/generate-brand.py
PNG/ICO rendering requires rsvg-convert; --svg-only skips raster exports.
The two source SVGs are never overwritten.
"""

import argparse
import shutil
import struct
import subprocess
import tempfile
from copy import deepcopy
from pathlib import Path
from xml.etree import ElementTree as ET

ASSETS = Path(__file__).resolve().parents[1] / "internal/web/static/brand"
TRAY = Path(__file__).resolve().parents[1] / "internal/desktop/assets"
NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", NS)


def elements(source, color=None):
    result = []
    for element in source:
        if element.tag in {f"{{{NS}}}title", f"{{{NS}}}desc"}:
            continue
        element = deepcopy(element)
        if color:
            # These masters use solid fills; preserve geometry and transforms.
            for node in element.iter():
                if "fill" in node.attrib and node.get("fill") != "none":
                    node.set("fill", color)
            element.set("fill", color)
        result.append(element)
    return result


def placed(source, x, y, scale=1, color=None):
    group = ET.Element(f"{{{NS}}}g", {
        "transform": f"translate({x} {y}) scale({scale})",
    })
    group.extend(elements(source, color))
    return group


def write(name, width, height, children):
    root = ET.Element(f"{{{NS}}}svg", {
        "viewBox": f"0 0 {width} {height}", "width": str(width),
        "height": str(height), "role": "img", "aria-labelledby": "title",
    })
    ET.SubElement(root, f"{{{NS}}}title", {"id": "title"}).text = "Driga — Murmur"
    root.extend(children)
    ET.indent(root, space="  ")
    (ASSETS / name).write_text(ET.tostring(root, encoding="unicode") + "\n")


def generate_icns(renderer, portable=False):
    """Render the unchanged Murmur master at Apple's 1x and 2x icon sizes."""
    iconutil = shutil.which("iconutil")
    if not portable and not iconutil:
        raise SystemExit("macOS iconutil is required to regenerate Driga.icns")
    with tempfile.TemporaryDirectory(prefix="driga-icon-") as temporary:
        iconset = Path(temporary) / "Driga.iconset"
        iconset.mkdir()
        for size in (16, 32, 128, 256, 512):
            for scale in (1, 2):
                suffix = "@2x" if scale == 2 else ""
                subprocess.run([
                    renderer, "--width", str(size * scale), "--height", str(size * scale),
                    "--output", str(iconset / f"icon_{size}x{size}{suffix}.png"),
                    str(ASSETS / "murmur.svg"),
                ], check=True)
        if portable:
            # PNG-backed ICNS elements retain every Apple 1x/2x representation.
            chunks = []
            types = ("icp4", "ic11", "icp5", "ic12", "ic07", "ic13", "ic08", "ic14", "ic09", "ic10")
            for path, kind in zip(
                (iconset / f"icon_{size}x{size}{suffix}.png"
                 for size in (16, 32, 128, 256, 512) for suffix in ("", "@2x")), types,
            ):
                png = path.read_bytes()
                chunks.append(kind.encode("ascii") + struct.pack(">I", len(png) + 8) + png)
            payload = b"".join(chunks)
            (ASSETS / "Driga.icns").write_bytes(b"icns" + struct.pack(">I", len(payload) + 8) + payload)
            return
        subprocess.run([
            iconutil, "--convert", "icns", "--output", str(ASSETS / "Driga.icns"),
            str(iconset),
        ], check=True)


def generate_tray(renderer):
    """Render the tray icons: a black template for the macOS menu bar, and ink
    and white marks, each also with a dot for "something needs you", for
    notification areas that show icons in color."""
    symbol = ET.parse(ASSETS / "murmur.svg").getroot()
    primary = symbol.find(f"{{{NS}}}g").get("fill")
    variants = [("tray-template.png", "#000000", False)]
    for tone, color in [("ink", "#1b1e21"), ("white", "#ffffff")]:
        variants += [(f"tray-{tone}.png", color, False), (f"tray-{tone}-dot.png", color, True)]
    TRAY.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="driga-tray-") as temporary:
        for name, color, dot in variants:
            root = ET.Element(f"{{{NS}}}svg", {"viewBox": "0 0 128 128", "width": "128", "height": "128"})
            mark = ET.SubElement(root, f"{{{NS}}}g")
            mark.extend(elements(symbol, color))
            if dot:
                # A gap around the dot keeps it apart from the mark at 16 points.
                mask = ET.SubElement(ET.SubElement(root, f"{{{NS}}}defs"), f"{{{NS}}}mask", {"id": "gap"})
                ET.SubElement(mask, f"{{{NS}}}rect", {"width": "128", "height": "128", "fill": "#ffffff"})
                ET.SubElement(mask, f"{{{NS}}}circle", {"cx": "104", "cy": "24", "r": "34", "fill": "#000000"})
                mark.set("mask", "url(#gap)")
                ET.SubElement(root, f"{{{NS}}}circle", {"cx": "104", "cy": "24", "r": "24", "fill": primary})
            source = Path(temporary) / (name + ".svg")
            source.write_text(ET.tostring(root, encoding="unicode"))
            subprocess.run([
                renderer, "--width", "32", "--height", "32", "--output", str(TRAY / name), str(source),
            ], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--svg-only", action="store_true")
    mode.add_argument("--icns-only", action="store_true", help="regenerate only Driga.icns (macOS)")
    mode.add_argument("--tray-only", action="store_true", help="regenerate only the desktop app's tray icons")
    parser.add_argument("--portable-icns", action="store_true", help="encode PNG ICNS chunks when iconutil cannot run")
    args = parser.parse_args()
    if args.portable_icns and not args.icns_only:
        parser.error("--portable-icns requires --icns-only")
    renderer = shutil.which("rsvg-convert")
    if not args.svg_only and not renderer:
        parser.error("rsvg-convert is required for PNG exports; use --svg-only for SVGs")

    if args.icns_only:
        generate_icns(renderer, args.portable_icns)
        return
    if args.tray_only:
        generate_tray(renderer)
        return

    symbol = ET.parse(ASSETS / "murmur.svg").getroot()
    wordmark = ET.parse(ASSETS / "wordmark.svg").getroot()
    primary = symbol.find(f"{{{NS}}}g").get("fill")
    ink = "#1b1e21"
    white = "#ffffff"

    for name, color in [("murmur-ink.svg", ink), ("murmur-white.svg", white)]:
        write(name, 128, 128, elements(symbol, color))
    for name, mark_color, type_color in [
        ("lockup.svg", None, ink),
        ("lockup-ink.svg", ink, ink),
        ("lockup-white.svg", white, white),
    ]:
        write(name, 360, 128, [
            placed(symbol, 0, 0, color=mark_color),
            placed(wordmark, 148, 29, color=type_color),
        ])
    write("lockup-stacked.svg", 256, 310, [
        placed(symbol, 32, 0, scale=1.5),
        placed(wordmark, 42, 216, color=ink),
    ])
    tile = ET.Element(f"{{{NS}}}rect", {
        "width": "64", "height": "64", "rx": "14", "fill": primary,
    })
    write("favicon.svg", 64, 64, [tile, placed(symbol, 3, 3, scale=58 / 128, color=white)])

    if not args.svg_only:
        # Render each size directly from the master, never from a scaled PNG.
        # ICO entries are PNG payloads, in fixed size order, with no timestamps
        # or metadata added by this packer. No Pillow/ImageMagick dependency.
        sizes = (16, 24, 32, 48, 64, 128, 256)
        images = [subprocess.run([
            renderer, "--width", str(size), "--height", str(size),
            str(ASSETS / "murmur.svg"),
        ], check=True, capture_output=True).stdout for size in sizes]
        offset = 6 + 16 * len(sizes)
        directory = bytearray(struct.pack("<HHH", 0, 1, len(sizes)))
        for size, image in zip(sizes, images):
            directory.extend(struct.pack(
                "<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32,
                len(image), offset,
            ))
            offset += len(image)
        (ASSETS / "murmur.ico").write_bytes(directory + b"".join(images))
        for source, destination, width in [
            ("favicon.svg", "favicon-32.png", 32),
            ("favicon.svg", "apple-touch-icon.png", 180),
            ("favicon.svg", "icon-512.png", 512),
            ("murmur.svg", "murmur-1024.png", 1024),
            ("lockup.svg", "lockup.png", 1200),
        ]:
            subprocess.run([
                renderer, "--width", str(width), "--output",
                str(ASSETS / destination), str(ASSETS / source),
            ], check=True)
        generate_tray(renderer)
    print("Generated Murmur variants from the canonical symbol and wordmark SVGs.")


if __name__ == "__main__":
    main()

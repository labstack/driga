#!/usr/bin/env python3
"""Generate brand variants from murmur.svg and wordmark.svg.

Run from any directory: rtk python3 scripts/generate-brand.py
PNG rendering requires rsvg-convert; --svg-only skips raster exports.
The two source SVGs are never overwritten.
"""

import argparse
from copy import deepcopy
from pathlib import Path
import shutil
import subprocess
from xml.etree import ElementTree as ET

ASSETS = Path(__file__).resolve().parents[1] / "internal/web/static/brand"
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--svg-only", action="store_true")
    args = parser.parse_args()
    renderer = shutil.which("rsvg-convert")
    if not args.svg_only and not renderer:
        parser.error("rsvg-convert is required for PNG exports; use --svg-only for SVGs")

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
    print("Generated Murmur variants from the canonical symbol and wordmark SVGs.")


if __name__ == "__main__":
    main()

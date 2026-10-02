"""Render captured TUI View ANSI output into PNG previews (requires Pillow).

Capture with SLOP_UI_PREVIEW_DIR and TestCaptureUIPreviews, then run:
    python docs/screenshots/render.py CAPTURE_DIR OUTPUT_DIR
"""
import re
import sys
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont

source, destination = map(Path, sys.argv[1:3])
destination.mkdir(parents=True, exist_ok=True)
font_path = Path("C:/Windows/Fonts/consola.ttf")
if not font_path.exists():
    font_path = Path("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf")
font = ImageFont.truetype(str(font_path), 16)
cell_width = round(font.getlength("M"))
cell_height = 23
palette = {
    24: "#005f87", 67: "#5f87af", 81: "#5fd7ff", 114: "#87d787",
    203: "#ff5f5f", 221: "#ffd75f", 231: "#ffffff", 245: "#8a8a8a",
}
for capture in source.glob("*.ansi"):
    lines = capture.read_text(encoding="utf-8").splitlines()
    plain = [re.sub(r"\x1b\[[0-9;]*m", "", line) for line in lines]
    image = Image.new("RGB", (max(map(len, plain)) * cell_width + 32,
                              len(lines) * cell_height + 32), "#10151c")
    draw = ImageDraw.Draw(image)
    for row, line in enumerate(lines):
        column = 0
        foreground, background = "#dae2eb", "#10151c"
        for token in re.split(r"(\x1b\[[0-9;]*m)", line):
            if token.startswith("\x1b["):
                codes = list(map(int, token[2:-1].split(";")))
                if codes == [0]:
                    foreground, background = "#dae2eb", "#10151c"
                for i in range(len(codes) - 2):
                    if codes[i:i+2] == [38, 5]:
                        foreground = palette.get(codes[i+2], foreground)
                    if codes[i:i+2] == [48, 5]:
                        background = palette.get(codes[i+2], background)
                continue
            for character in token:
                x, y = 16 + column * cell_width, 16 + row * cell_height
                draw.rectangle((x, y, x + cell_width, y + cell_height), fill=background)
                draw.text((x, y), character, font=font, fill=foreground)
                column += 1
    image.save(destination / (capture.stem + ".png"))

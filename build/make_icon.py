"""Draws the NameSmith icon (assets/icon.png and build/icon.ico).

Run: python3 build/make_icon.py   (needs Pillow)
"""
from PIL import Image, ImageDraw, ImageFilter

S = 1024  # draw big, scale down for crisp edges


def lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(len(a)))


def draw(size=S):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    k = size / 1024

    # rounded-square background with a diagonal blue → indigo gradient
    grad = Image.new("RGBA", (size, size))
    gd = ImageDraw.Draw(grad)
    top, bottom = (59, 130, 246, 255), (79, 70, 229, 255)
    for y in range(size):
        gd.line([(0, y), (size, y)], fill=lerp(top, bottom, y / size))
    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle([int(40 * k), int(40 * k), int(984 * k), int(984 * k)], radius=int(210 * k), fill=255)
    img.paste(grad, (0, 0), mask)

    d = ImageDraw.Draw(img)

    # soft shadow under the document
    sh = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    ImageDraw.Draw(sh).rounded_rectangle([int(250 * k), int(215 * k), int(735 * k), int(835 * k)], radius=int(60 * k), fill=(20, 20, 70, 110))
    sh = sh.filter(ImageFilter.GaussianBlur(int(28 * k)))
    img.alpha_composite(sh)
    d = ImageDraw.Draw(img)

    # white document with a folded corner
    x0, y0, x1, y1, fold = 235 * k, 190 * k, 720 * k, 810 * k, 150 * k
    d.rounded_rectangle([x0, y0, x1, y1], radius=int(56 * k), fill=(255, 255, 255, 255))
    d.polygon([(x1 - fold, y0), (x1, y0 + fold), (x1, y0), ], fill=(255, 255, 255, 0))
    # cut the corner (paint background colour over it) then draw the fold
    corner = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    ImageDraw.Draw(corner).polygon([(x1 - fold, y0 - 2), (x1 + 2, y0 - 2), (x1 + 2, y0 + fold)], fill=(255, 255, 255, 255))
    cm = corner.split()[3]
    img.paste(grad, (0, 0), Image.composite(cm, Image.new("L", (size, size), 0), mask))
    d = ImageDraw.Draw(img)
    d.polygon([(x1 - fold, y0), (x1 - fold, y0 + fold - 20 * k), (x1 - fold + 20 * k, y0 + fold), (x1, y0 + fold)], fill=(199, 210, 254, 255))

    # "text" lines: old name (grey) and new numbered names (blue)
    lx = x0 + 70 * k
    d.rounded_rectangle([lx, 330 * k, x1 - 210 * k, 380 * k], radius=int(25 * k), fill=(203, 213, 225, 255))
    for i, y in enumerate((470, 580, 690)):
        d.rounded_rectangle([lx, y * k, lx + 90 * k, (y + 56) * k], radius=int(18 * k), fill=(37, 99, 235, 255))
        d.rounded_rectangle([lx + 120 * k, (y + 6) * k, x1 - 80 * k - i * 40 * k, (y + 50) * k], radius=int(22 * k), fill=(147, 197, 253, 255))

    # orange pencil crossing the lower-right corner
    pencil = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    pd = ImageDraw.Draw(pencil)
    cx, cy, L, W = 700 * k, 690 * k, 330 * k, 92 * k
    body = [(cx - L / 2, cy - W / 2), (cx + L / 2, cy - W / 2), (cx + L / 2, cy + W / 2), (cx - L / 2, cy + W / 2)]
    pd.polygon(body, fill=(251, 146, 60, 255))
    pd.polygon([(cx - L / 2, cy - W / 2), (cx + L / 2, cy - W / 2), (cx + L / 2, cy - W / 6), (cx - L / 2, cy - W / 6)], fill=(253, 186, 116, 255))
    pd.polygon([(cx + L / 2, cy - W / 2), (cx + L / 2 + 70 * k, cy - W / 2), (cx + L / 2 + 70 * k, cy + W / 2), (cx + L / 2, cy + W / 2)], fill=(244, 114, 182, 255))
    tip = cx - L / 2
    pd.polygon([(tip, cy - W / 2), (tip - 110 * k, cy), (tip, cy + W / 2)], fill=(254, 243, 199, 255))
    pd.polygon([(tip - 72 * k, cy - W / 6.5), (tip - 110 * k, cy), (tip - 72 * k, cy + W / 6.5)], fill=(30, 41, 59, 255))
    pencil = pencil.rotate(38, center=(cx, cy), resample=Image.BICUBIC)
    psh = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    psh.paste((15, 15, 60, 120), (0, 0), pencil.split()[3])
    psh = psh.filter(ImageFilter.GaussianBlur(int(16 * k)))
    img.alpha_composite(psh, (int(10 * k), int(18 * k)))
    img.alpha_composite(pencil)
    return img


big = draw()
big.resize((512, 512), Image.LANCZOS).save("assets/icon.png")
sizes = [16, 20, 24, 32, 40, 48, 64, 128, 256]
frames = [big.resize((s, s), Image.LANCZOS) for s in sizes]
frames[-1].save("build/icon.ico", format="ICO", sizes=[(s, s) for s in sizes], append_images=frames[:-1])
# small preview strip for checking
strip = Image.new("RGBA", (sum(sizes) + 10 * len(sizes), 256), (240, 240, 240, 255))
x = 0
for f in frames:
    strip.alpha_composite(f, (x, 256 - f.size[1]))
    x += f.size[0] + 10
strip.save("/tmp/claude-0/-home-claude/ed33f483-7da5-5734-829e-faaf93067a05/scratchpad/icon_strip.png")
print("ok")

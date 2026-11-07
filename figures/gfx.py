"""Drawing toolkit shared by the paper figures: lane panels, outlined boxes, badges, arrows."""
import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.patches import FancyBboxPatch, Polygon, Ellipse, Circle, Rectangle, FancyArrowPatch

plt.rcParams.update({
    "font.family": "Noto Sans",
    "font.size": 8,
    "axes.unicode_minus": False,
    "mathtext.fontset": "dejavusans",
    "savefig.dpi": 400,
    "svg.fonttype": "none",
})

INK = "#222B38"
MUTED = "#5A6575"
LANE_FILL = "#F7F8FA"
LANE_EDGE = "#E1E5EB"

# kind -> (fill, edge)
KIND = {
    "blue": ("#E4EEFF", "#3B72E8"),
    "green": ("#DDF6E5", "#27AE60"),
    "teal": ("#D6F4EC", "#16A98A"),
    "orange": ("#FFEAD6", "#E8641B"),
    "purple": ("#EEE7FF", "#7C48E0"),
    "yellow": ("#FFF4C2", "#D9A400"),
    "slate": ("#EDF0F5", "#4B5668"),
    "red": ("#FFE6E6", "#DC3030"),
    "white": ("#FFFFFF", "#4B5668"),
}
FLOW = "#2B3440"
ACCENT = "#E8641B"
DASH = "#6C7685"


class Node:
    def __init__(s, cx, cy, w, h):
        s.cx, s.cy, s.w, s.h = cx, cy, w, h

    l = property(lambda s: (s.cx - s.w / 2, s.cy))
    r = property(lambda s: (s.cx + s.w / 2, s.cy))
    t = property(lambda s: (s.cx, s.cy + s.h / 2))
    b = property(lambda s: (s.cx, s.cy - s.h / 2))

    def at(s, side, off=0.0):
        """Edge point; off shifts along the edge (x for t/b, y for l/r)."""
        x, y = getattr(s, side)
        return (x + off, y) if side in "tb" else (x, y + off)


class Canvas:
    def __init__(s, w, h):
        s.w, s.h = w, h
        s.fig = plt.figure(figsize=(w, h))
        s.ax = s.fig.add_axes([0, 0, 1, 1])
        s.ax.set_xlim(0, w)
        s.ax.set_ylim(0, h)
        s.ax.axis("off")
        s._texts = []

    # ---- containers -------------------------------------------------
    def lane(s, x0, y0, x1, y1, title, size=8.2, vertical=False):
        s.ax.add_patch(FancyBboxPatch((x0, y0), x1 - x0, y1 - y0,
                       boxstyle="round,pad=0,rounding_size=0.09",
                       fc=LANE_FILL, ec=LANE_EDGE, lw=1.0, zorder=0))
        if title and vertical:
            s.ax.text(x0 + 0.13, (y0 + y1) / 2, title, ha="center", va="center", rotation=90,
                      fontsize=size, fontweight="semibold", color=MUTED, zorder=1)
        elif title:
            s.ax.text(x0 + 0.14, y1 - 0.12, title, ha="left", va="top", fontsize=size,
                      fontweight="semibold", color=MUTED, zorder=1)

    # ---- nodes ------------------------------------------------------
    def box(s, cx, cy, w, h, title, sub="", kind="blue", dashed=False, badge=None,
            tsize=8.6, ssize=7.0, center=False, lw=1.7, anchor_top=False):
        fc, ec = KIND[kind]
        n = Node(cx, cy, w, h)
        s.ax.add_patch(FancyBboxPatch((cx - w / 2, cy - h / 2), w, h,
                       boxstyle="round,pad=0,rounding_size=0.08", fc=fc, ec=ec, lw=lw,
                       ls=(0, (3.2, 2.2)) if dashed else "-", zorder=3))
        pad = 0.11
        tl = title.count("\n") + 1
        sl = (sub.count("\n") + 1) if sub else 0
        th, sh = tl * tsize * 1.25 / 72, sl * ssize * 1.32 / 72
        gap = 0.05 if sub else 0
        top = cy + (th + gap + sh) / 2 if (center or not sub) and not anchor_top else cy + h / 2 - pad
        ha, x = ("center", cx) if center else ("left", cx - w / 2 + pad + 0.02)
        t1 = s.ax.text(x, top, title, ha=ha, va="top", fontsize=tsize, fontweight="bold",
                       color=INK, zorder=5, linespacing=1.2)
        s._texts.append((t1, n))
        if sub:
            t2 = s.ax.text(x, top - th - gap, sub, ha=ha, va="top", fontsize=ssize,
                           color=MUTED, zorder=5, linespacing=1.28)
            s._texts.append((t2, n))
        if badge is not None:
            s.badge(cx + w / 2 - 0.02, cy + h / 2 - 0.02, badge, ec)
        return n

    def badge(s, x, y, text, color, r=0.115):
        s.ax.add_patch(Circle((x, y), r, fc=color, ec="white", lw=1.4, zorder=7))
        s.ax.text(x, y - 0.004, str(text), ha="center", va="center", color="white",
                  fontsize=7, fontweight="bold", zorder=8)

    def chip(s, cx, cy, w, h, text, kind="white", size=7.2):
        fc, ec = KIND[kind]
        s.ax.add_patch(FancyBboxPatch((cx - w / 2, cy - h / 2), w, h,
                       boxstyle="round,pad=0,rounding_size=0.06", fc="white", ec=ec, lw=1.3, zorder=5))
        s.ax.text(cx, cy, text, ha="center", va="center", fontsize=size, fontweight="semibold",
                  color=INK, zorder=6)

    def diamond(s, cx, cy, w, h, text, kind="yellow", badge=None, size=7.2):
        fc, ec = KIND[kind]
        s.ax.add_patch(Polygon([(cx, cy + h / 2), (cx + w / 2, cy), (cx, cy - h / 2), (cx - w / 2, cy)],
                       closed=True, fc=fc, ec=ec, lw=1.7, joinstyle="round", zorder=3))
        s.ax.text(cx, cy, text, ha="center", va="center", fontsize=size, fontweight="bold",
                  color=INK, zorder=5, linespacing=1.15)
        if badge is not None:
            s.badge(cx + w * 0.30, cy + h * 0.30, badge, ec)
        return Node(cx, cy, w, h)

    def store(s, cx, cy, w, h, title, sub="", kind="slate"):
        fc, ec = KIND[kind]
        ry = 0.08
        a = s.ax
        a.add_patch(Rectangle((cx - w / 2, cy - h / 2 + ry), w, h - 2 * ry, fc=fc, ec="none", zorder=3))
        a.add_patch(Ellipse((cx, cy - h / 2 + ry), w, 2 * ry, fc=fc, ec=ec, lw=1.7, zorder=3))
        a.plot([cx - w / 2] * 2, [cy - h / 2 + ry, cy + h / 2 - ry], color=ec, lw=1.7, zorder=4,
               solid_capstyle="butt")
        a.plot([cx + w / 2] * 2, [cy - h / 2 + ry, cy + h / 2 - ry], color=ec, lw=1.7, zorder=4,
               solid_capstyle="butt")
        a.add_patch(Ellipse((cx, cy + h / 2 - ry), w, 2 * ry, fc="white", ec=ec, lw=1.7, zorder=4))
        n = Node(cx, cy, w, h)
        body_top = cy + h / 2 - 2 * ry
        t1 = a.text(cx, body_top - 0.03, title, ha="center", va="top", fontsize=8, fontweight="bold",
                    color=INK, zorder=5)
        s._texts.append((t1, n))
        if sub:
            t2 = a.text(cx, body_top - 0.03 - 0.17, sub, ha="center", va="top", fontsize=6.8,
                        color=MUTED, zorder=5, linespacing=1.25)
            s._texts.append((t2, n))
        return n

    def actor(s, x, y, label, color=INK, size=7.6):
        a = s.ax
        k = 1.0
        a.add_patch(Circle((x, y + 0.60 * k), 0.12, fc="white", ec=color, lw=1.6, zorder=4))
        for (xa, ya, xb, yb) in [(0, 0.48, 0, 0.14), (-0.22, 0.36, 0.22, 0.36), (0, 0.14, -0.18, -0.14),
                                 (0, 0.14, 0.18, -0.14)]:
            a.plot([x + xa, x + xb], [y + ya, y + yb], color=color, lw=1.6, solid_capstyle="round", zorder=4)
        a.text(x, y - 0.24, label, ha="center", va="top", fontsize=size, fontweight="semibold", color=INK,
               linespacing=1.2)
        return Node(x, y + 0.2, 0.5, 0.9)

    # ---- connectors -------------------------------------------------
    def arrow(s, pts, color=FLOW, dashed=False, lw=1.25, label=None, lpos=None, lsize=6.8,
              head=True, lha="center", lva="center", lcolor=INK, dot_start=False):
        xs, ys = zip(*pts)
        ls = (0, (4, 2.6)) if dashed else "-"
        s.ax.plot(xs, ys, color=color, lw=lw, ls=ls, zorder=2, solid_joinstyle="round",
                  dash_joinstyle="round", solid_capstyle="butt")
        if head:
            s.ax.add_patch(FancyArrowPatch(pts[-2], pts[-1], arrowstyle="-|>,head_length=3.6,head_width=2.2",
                           mutation_scale=1.5, color=color, lw=lw, zorder=2, shrinkA=0, shrinkB=0))
        if dot_start:
            s.ax.add_patch(Circle(pts[0], 0.035, fc=color, ec="none", zorder=3))
        if label:
            x, y = lpos if lpos else ((pts[0][0] + pts[1][0]) / 2, (pts[0][1] + pts[1][1]) / 2)
            s.text(x, y, label, size=lsize, ha=lha, va=lva, color=lcolor)

    def text(s, x, y, txt, size=7, ha="center", va="center", color=INK, weight="semibold", bg=True, **kw):
        s.ax.text(x, y, txt, ha=ha, va=va, fontsize=size, fontweight=weight, color=color, zorder=6,
                  bbox=dict(fc=LANE_FILL if bg else "none", ec="none", pad=1.2) if bg else None, **kw)

    def legend(s, x, y, items, gap=0.18, size=6.8):
        """items: (label, color, dashed). Laid out left to right from x."""
        r = s.fig.canvas.get_renderer()
        for lab, col, dashed in items:
            s.ax.plot([x, x + 0.34], [y, y], color=col, lw=1.6, ls=(0, (3.2, 2.2)) if dashed else "-",
                      solid_capstyle="butt")
            t = s.ax.text(x + 0.42, y, lab, ha="left", va="center", fontsize=size, fontweight="semibold",
                          color=INK)
            bb = t.get_window_extent(r).transformed(s.ax.transData.inverted())
            x = bb.x1 + gap

    # ---- output -----------------------------------------------------
    def check(s):
        r = s.fig.canvas.get_renderer()
        inv = s.ax.transData.inverted()
        bad = []
        for t, n in s._texts:
            bb = t.get_window_extent(r).transformed(inv)
            if (bb.x0 < n.cx - n.w / 2 + 0.03 or bb.x1 > n.cx + n.w / 2 - 0.03 or
                    bb.y0 < n.cy - n.h / 2 + 0.02 or bb.y1 > n.cy + n.h / 2 - 0.02):
                bad.append(t.get_text().replace("\n", " / "))
        return bad

    def save(s, path):
        bad = s.check()
        if bad:
            print("  OVERFLOW in", path, "->", bad)
        s.fig.savefig(path, dpi=400, facecolor="white")
        plt.close(s.fig)

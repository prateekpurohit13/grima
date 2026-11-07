"""Figures 6-10: result charts. Every number is copied from a table in the paper."""
import matplotlib.pyplot as plt
from matplotlib.patches import Patch

import gfx  # noqa: F401  (sets fonts and rcParams)
from gfx import INK, MUTED, KIND, ACCENT

MONO = {"family": ["Liberation Mono", "DejaVu Sans Mono"]}
BLUE, GREEN, TEAL, ORANGE, PURPLE, RED, SLATE, YELLOW = (KIND[k][1] for k in
    ("blue", "green", "teal", "orange", "purple", "red", "slate", "yellow"))
GRID = "#E3E7ED"


def _fig(w, h, **kw):
    fig, axes = plt.subplots(figsize=(w, h), **kw)
    return fig, axes


def _clean(ax, grid="y"):
    for s in ("top", "right"):
        ax.spines[s].set_visible(False)
    for s in ("left", "bottom"):
        ax.spines[s].set_color("#9AA3AF")
    ax.tick_params(colors=MUTED, labelsize=7, length=3)
    ax.set_axisbelow(True)
    ax.grid(axis=grid, color=GRID, lw=0.8)


def _title(ax, text):
    ax.set_title(text, loc="left", fontsize=8.4, fontweight="bold", color=INK, pad=8)


def _save(fig, path):
    fig.savefig(path, dpi=400, facecolor="white")
    plt.close(fig)


def fig6(out):
    """Attribution accuracy: mechanism and matching delay."""
    fig, (a, b) = plt.subplots(1, 2, figsize=(6.5, 3.0), gridspec_kw={"width_ratios": [1.05, 1], "wspace": 0.5})
    rows = [("Correlation on\nwrite volume", 0.0, "0 of 1,255", RED),
            ("Windows file-system\nauditing", 96.9, "945 of 975", BLUE),
            ("of which\ncausal", 100.0, "945 of 945", TEAL)]
    for i, (n, v, lab, col) in enumerate(rows):
        a.barh(i, v, color=col, height=0.55, zorder=3)
        a.text(max(v, 0) + 2, i, f"{v:.1f}%  ({lab})" if v else f"0.0%  ({lab})", va="center", fontsize=7,
               fontweight="bold", color=INK)
    a.set_yticks(range(3), [r[0] for r in rows])
    a.invert_yaxis()
    a.set_xlim(0, 150)
    a.set_xticks([0, 25, 50, 75, 100])
    a.set_xlabel("Decisions attributed correctly (%)", fontsize=7.2, color=MUTED)
    _clean(a, "x")
    _title(a, "(a) By mechanism")

    d = ["100 ms", "300 ms", "500 ms", "1 s"]
    acc = [90.4, 96.9, 98.5, 84.6]
    causal = [100, 100, 100, 84.9]
    cols = [BLUE, BLUE, GREEN, BLUE]
    b.bar(range(4), acc, color=cols, width=0.58, zorder=3)
    for i, v in enumerate(acc):
        b.text(i, v / 2, f"{v:.1f}", ha="center", va="center", fontsize=7.4, fontweight="bold", color="white",
               zorder=5)
    b.plot(range(4), causal, ls="none", marker="D", ms=5.5, mfc="white", mec=INK, mew=1.4, zorder=6)
    b.annotate("shipped\ndefault", (2, 98.5), (2, 114), ha="center", va="center", fontsize=6.8,
               fontweight="bold", color=GREEN, arrowprops=dict(arrowstyle="-", color=GREEN, lw=1.0))
    b.set_xticks(range(4), d)
    b.set_ylim(0, 122)
    b.set_yticks([0, 25, 50, 75, 100])
    b.set_xlabel("Matching delay", fontsize=7.2, color=MUTED)
    b.set_ylabel("Accuracy (%)", fontsize=7.2, color=MUTED)
    _clean(b, "y")
    _title(b, "(b) By matching delay")
    b.legend(handles=[Patch(fc=BLUE, label="all decisions"), plt.Line2D([], [], ls="none", marker="D", ms=5,
             mfc="white", mec=INK, mew=1.3, label="causal decisions only")], loc="lower left",
             bbox_to_anchor=(0.0, -0.36), ncol=2, frameon=False, fontsize=6.8, handlelength=1.2,
             columnspacing=1.4)
    fig.subplots_adjust(left=0.2, right=0.98, top=0.88, bottom=0.27)
    _save(fig, out)


def fig7(out):
    """Verdict score per scenario against the level bands."""
    fig, ax = plt.subplots(figsize=(6.5, 3.6))
    bands = [(20, 45, "low", "green"), (45, 70, "medium", "yellow"), (70, 88, "high", "orange"),
             (88, 100, "critical", "red")]
    for a0, a1, n, k in bands:
        ax.axvspan(a0, a1, color=KIND[k][0], zorder=0, lw=0)
        ax.axvline(a0, color=KIND[k][1], lw=0.9, ls=(0, (3, 2)), zorder=1)
        ax.text((a0 + a1) / 2, -0.82, n, ha="center", va="center", fontsize=6.8, fontweight="bold",
                color=KIND[k][1])
    ax.text(10, -0.82, "info", ha="center", va="center", fontsize=6.8, fontweight="bold", color=MUTED)
    rows = [
        ("Burst", 100.0, None, BLUE, "100.0"),
        ("Intermittent, first 4 KiB", 100.0, None, BLUE, "100.0"),
        ("Tail-only overwrite", 94.1, None, BLUE, "94.1"),
        ("Drip, 1 file per 5 s", 45.9, 49.9, BLUE, "45.9 to 49.9"),
        ("Split, 8 children (host filing)", 95.8, None, BLUE, "95.8"),
        ("Split: each child (causal)", 21.4, (18.6, 23.4), PURPLE, "21.4 (18.6 to 23.4)"),
        ("Split: tree aggregate (causal)", 100.0, None, PURPLE, "100.0"),
    ]
    for i, (n, v, rng, col, lab) in enumerate(rows):
        ax.barh(i, v, color=col, height=0.56, zorder=3)
        if isinstance(rng, float):
            ax.barh(i, rng - v, left=v, color=col, alpha=0.35, height=0.56, zorder=3)
        if isinstance(rng, tuple):
            ax.plot(rng, [i, i], color=INK, lw=1.2, zorder=6)
            for r in rng:
                ax.plot([r, r], [i - 0.15, i + 0.15], color=INK, lw=1.2, zorder=6)
        if v > 90:
            ax.text(v - 1.2, i, lab, ha="right", va="center", fontsize=7, fontweight="bold", color="white",
                    zorder=7)
        else:
            x = (rng if isinstance(rng, float) else rng[1] if rng else v) + 1.6
            ax.text(x, i, lab, ha="left", va="center", fontsize=7, fontweight="bold", color=INK, zorder=7)
    ax.set_yticks(range(len(rows)), [r[0] for r in rows])
    ax.invert_yaxis()
    ax.set_ylim(len(rows) - 0.4, -1.25)
    ax.set_xlim(0, 100)
    ax.set_xticks([0, 20, 45, 70, 88, 100])
    ax.set_xlabel("Peak verdict score", fontsize=7.2, color=MUTED)
    _clean(ax, "x")
    ax.grid(False)
    ax.legend(handles=[Patch(fc=BLUE, label="host-level filing"), Patch(fc=PURPLE, label="causal attribution")],
              loc="lower center", bbox_to_anchor=(0.5, -0.27), ncol=2, frameon=False, fontsize=7,
              handlelength=1.2)
    fig.subplots_adjust(left=0.30, right=0.98, top=0.97, bottom=0.2)
    _save(fig, out)


def fig8(out):
    """Signal-subset ablation."""
    fig, (a, b) = plt.subplots(1, 2, figsize=(6.5, 3.2), gridspec_kw={"wspace": 0.38})
    names = ["rules_only", "+entropy", "+rename", "+calibration", "all"]
    dr = [0.0, 1.0, None, 1.0, 1.0]
    fpr = [0.0, 0.0, 10568.8, 1379.1, 5139.0]
    cols = [SLATE, GREEN, RED, TEAL, BLUE]
    x = range(5)
    for i, v in enumerate(dr):
        if v is None:
            a.bar(i, 1.0, width=0.58, fc="white", ec=RED, hatch="////", lw=1.1, zorder=3)
            a.text(i, 0.5, "no\nthreshold\nmeets 1%", ha="center", va="center", fontsize=6.0,
                   fontweight="bold", color=RED, zorder=5, bbox=dict(fc="white", ec="none", pad=1.2))
        else:
            a.bar(i, v, width=0.58, color=cols[i], zorder=3)
            a.text(i, v + 0.03, f"{v:.1f}", ha="center", va="bottom", fontsize=7.2, fontweight="bold", color=INK)
    a.set_ylim(0, 1.2)
    a.set_yticks([0, 0.25, 0.5, 0.75, 1.0])
    a.set_ylabel("Detection rate at 1% FPR", fontsize=7.2, color=MUTED)
    a.set_xticks(x, names, rotation=35, ha="right", fontfamily=MONO["family"], fontsize=6.8)
    _clean(a, "y")
    _title(a, "(a) Detection")

    floor = 1.0
    for i, v in enumerate(fpr):
        if v == 0:
            b.plot([i], [floor], marker="o", ms=5, color=GREEN if i else SLATE, zorder=5, clip_on=False)
            b.text(i, floor * 2.2, "0.0", ha="center", va="bottom", fontsize=7.2, fontweight="bold", color=INK)
        else:
            b.bar(i, v - floor, bottom=floor, width=0.58, color=cols[i], zorder=3)
            b.text(i, v * 1.25, f"{v:,.1f}", ha="center", va="bottom", fontsize=7.0, fontweight="bold",
                   color=INK)
    b.set_yscale("log")
    b.set_ylim(floor, 6e4)
    b.set_ylabel("False positives per 24 h (log scale)", fontsize=7.2, color=MUTED)
    b.set_xticks(x, names, rotation=35, ha="right", fontfamily=MONO["family"], fontsize=6.8)
    _clean(b, "y")
    b.tick_params(axis="y", which="minor", length=0)
    _title(b, "(b) False positives")
    b.annotate("", xy=(3, 14000), xytext=(4, 14000), arrowprops=dict(arrowstyle="-", color=MUTED, lw=1.0))
    b.text(3.5, 17500, "not distinguishable\nat one round", ha="center", va="bottom", fontsize=6.2, color=MUTED,
           style="italic")
    fig.subplots_adjust(left=0.1, right=0.99, top=0.9, bottom=0.27)
    _save(fig, out)


def fig9(out):
    """Real families: data written before detection."""
    fig, ax = plt.subplots(figsize=(6.5, 2.7))
    fam = [("LockBit 3.0", "bulk, rename", 384 / 1024, 1.2, "384 KiB", "1.2 MB", "5/5", BLUE),
           ("Conti", "bulk, partial", 512 / 1024, 1.6, "512 KiB", "1.6 MB", "5/5", BLUE),
           ("WannaCry", "bulk, burst", 256 / 1024, 896 / 1024, "256 KiB", "896 KiB", "5/5", BLUE),
           ("BlackCat", "intermittent", 2.3, 6.4, "2.3 MB", "6.4 MB", "4/5", ORANGE)]
    for i, (n, shape, med, mx, ml, xl, d, col) in enumerate(fam):
        ax.plot([med, mx], [i, i], color=INK, lw=1.3, zorder=4)
        ax.plot([mx, mx], [i - 0.16, i + 0.16], color=INK, lw=1.3, zorder=4)
        ax.scatter([med], [i], s=70, color=col, edgecolor="white", linewidth=1.2, zorder=5)
        ax.text(med / 1.1, i - 0.3, ml, ha="right", va="center", fontsize=6.8, fontweight="bold", color=INK)
        ax.text(mx * 1.1, i, xl, ha="left", va="center", fontsize=6.8, color=MUTED)
        ax.text(1.04, i, d, transform=ax.get_yaxis_transform(), ha="left", va="center", fontsize=8,
                fontweight="bold", color=INK)
    ax.set_yticks(range(4), [f[0] for f in fam], fontsize=7.6, fontweight="bold")
    for lbl, f in zip(ax.get_yticklabels(), fam):
        lbl.set_color(INK)
    for i, f in enumerate(fam):
        ax.text(-0.015, i + 0.28, f[1], transform=ax.get_yaxis_transform(), ha="right", va="center",
                fontsize=6.4, color=MUTED)
    ax.text(1.04, -0.78, "detected", transform=ax.get_yaxis_transform(), ha="left", va="center", fontsize=6.4,
            color=MUTED)
    ax.invert_yaxis()
    ax.set_ylim(3.6, -1.0)
    ax.set_xscale("log")
    ax.set_xlim(0.15, 14)
    ax.set_xticks([0.25, 0.5, 1, 2, 4, 8], ["0.25", "0.5", "1", "2", "4", "8"])
    ax.minorticks_off()
    ax.set_xlabel("Data written before detection (MB, log scale)", fontsize=7.2, color=MUTED)
    _clean(ax, "x")
    ax.legend(handles=[plt.Line2D([], [], marker="o", ls="none", color=BLUE, ms=6, label="bulk encryptor, median"),
                       plt.Line2D([], [], marker="o", ls="none", color=ORANGE, ms=6, label="intermittent, median"),
                       plt.Line2D([], [], color=INK, lw=1.3, label="to the maximum over five rounds")],
              loc="upper left", bbox_to_anchor=(0.0, 1.14), ncol=3, frameon=False, fontsize=6.8,
              handletextpad=0.4, columnspacing=1.2)
    fig.subplots_adjust(left=0.2, right=0.89, top=0.88, bottom=0.2)
    _save(fig, out)


def fig10(out):
    """Detector overhead."""
    fig, (a, b) = plt.subplots(1, 2, figsize=(6.5, 3.0), gridspec_kw={"wspace": 0.32})
    ph = ["Detector\nidle", "Detector\nunder load", "Workload,\nno detector", "Workload,\nwith detector"]
    cols = [PURPLE, PURPLE, SLATE, SLATE]
    data = [(a, [3.25, 6.11, 7.07, 7.13], [5.74, 12.47, 16.73, 12.60], "CPU (% of one core)", "{:.2f}%",
             "(a) CPU", 20),
            (b, [131, 159, 15, 15], [143, 172, 27, 29], "Resident set size (MiB)", "{:d}", "(b) Memory", 205)]
    for ax, mean, peak, yl, fmt, ttl, top in data:
        ax.bar(range(4), mean, color=cols, width=0.56, zorder=3)
        for i, (m, p) in enumerate(zip(mean, peak)):
            ax.plot([i - 0.28, i + 0.28], [p, p], color=INK, lw=1.6, zorder=6)
            ax.plot([i, i], [m, p], color=INK, lw=1.0, zorder=5)
            ax.text(i, p + top * 0.025, fmt.format(m if fmt != "{:d}" else int(m)), ha="center", va="bottom",
                    fontsize=7.2, fontweight="bold", color=INK)
        ax.set_xticks(range(4), ph, fontsize=6.4)
        ax.set_ylim(0, top)
        ax.set_ylabel(yl, fontsize=7.2, color=MUTED)
        _clean(ax, "y")
        _title(ax, ttl)
    a.legend(handles=[Patch(fc=PURPLE, label="detector alone"), Patch(fc=SLATE, label="benign workload"),
                      plt.Line2D([], [], color=INK, lw=1.6, label="peak")], loc="upper left",
             bbox_to_anchor=(0.0, 1.0), frameon=False, fontsize=6.6, handlelength=1.2)
    fig.subplots_adjust(left=0.08, right=0.99, top=0.9, bottom=0.2)
    _save(fig, out)

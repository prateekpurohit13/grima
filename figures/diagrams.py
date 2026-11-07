"""Figures 1-5: the design diagrams. Run via make_figures.py."""
from matplotlib.patches import Ellipse, Circle, Rectangle

from gfx import Canvas, FLOW, ACCENT, DASH, INK, MUTED, KIND


def fig1(out):
    """Use cases and the actors that reach them."""
    c = Canvas(6.5, 5.4)
    o = 0.4
    c.lane(1.45, 0.62, 5.05, 5.25, "GRIMA")
    ux = 3.25
    cases = [
        (4.5, "Calibrate or recalibrate\nthe host baseline", "purple"),
        (3.85, "Alert above the\nconfigured level", "blue"),
        (3.2, "Inspect the timeline,\nfindings and health", "blue"),
        (2.55, "Suspend a critical\nprocess (privileged)", "blue"),
        (1.9, "Reproduce an evaluation", "green"),
        (1.25, "Monitor activity and\nscore actors", "teal"),
    ]
    ys = []
    for y, txt, kind in cases:
        fc, ec = KIND[kind]
        c.ax.add_patch(Ellipse((ux, y), 2.55, 0.54, fc=fc, ec=ec, lw=1.7, zorder=3))
        c.ax.text(ux, y, txt, ha="center", va="center", fontsize=7.6, fontweight="bold", color=INK,
                  zorder=5, linespacing=1.15)
        ys.append(y)
    op, ad, wl = (0.62, 3.4), (5.88, 2.9), (0.62, 1.05)
    c.actor(*op, "Security\noperator")
    c.actor(*ad, "Host\nadministrator")
    c.actor(*wl, "Monitored\nworkload")
    for i in (0, 1, 2, 3):
        c.ax.plot([op[0] + 0.22, ux - 1.2], [op[1] + 0.36, ys[i]], color=FLOW, lw=1.1, zorder=1)
    for i in (0, 4, 5):
        c.ax.plot([ad[0] - 0.22, ux + 1.2], [ad[1] + 0.36, ys[i]], color=FLOW, lw=1.1, zorder=1)
    c.ax.plot([wl[0] + 0.22, ux - 1.2], [wl[1] + 0.36, ys[5]], color=FLOW, lw=1.1, zorder=1)
    c.legend(0.2, 0.2, [("operator", KIND["blue"][1], False), ("administrator", KIND["green"][1], False),
                        ("operator and administrator", KIND["purple"][1], False),
                        ("administrator and workload", KIND["teal"][1], False)])
    c.save(out)


def fig2(out):
    """Layered architecture."""
    c = Canvas(6.5, 8.0)
    L, R = 0.05, 6.45
    c.lane(L, 5.9, R, 7.95, "Observe", vertical=True)
    c.lane(L, 3.55, R, 5.82, "Transport and fingerprint", vertical=True)
    c.lane(L, 1.42, R, 3.47, "Score and fuse", vertical=True)
    c.lane(L, 0.45, R, 1.34, "Respond", vertical=True)

    plat = c.box(1.95, 7.3, 2.6, 0.6, "Platform detection", "selects the OS adapters", "teal")
    cfg = c.store(5.4, 7.3, 1.5, 0.74, "Config (TOML)", "thresholds, weights", "slate")
    sens = c.box(3.35, 6.42, 5.5, 0.82, "Layer 1: sensors (user space only)", "", "teal", badge=1, anchor_top=True)
    for i, t in enumerate(["file", "process", "persistence", "decoys"]):
        c.chip(1.35 + i * 1.3, 6.2, 1.1, 0.26, t)
    bus = c.box(3.35, 5.35, 5.5, 0.62, "Layer 2: bounded event bus",
                "one frozen Event schema; drop policy; every drop counted", "yellow", badge=2)
    rules = c.box(1.5, 4.25, 1.65, 0.86, "Rule layer", "9 hard rules; each\nsets an Override floor", "slate")
    fp = c.box(3.5, 4.25, 1.75, 0.86, "Layer 2b: fingerprint", "per actor: window plus\ncumulative counters",
               "purple", badge="2b")
    tree = c.box(5.45, 4.25, 1.65, 0.86, "Tree aggregator", "sums a process group\nbefore scoring", "purple")
    score = c.box(3.35, 2.85, 5.5, 0.62, "Layer 3: signal scorer", "13 signals; deviations against the baseline",
                  "blue", badge=3)
    fuse = c.box(2.35, 1.82, 3.5, 0.62, "Fusion", "noisy-OR, Primary gate, override floor, bands 20/45/70/88",
                 "blue")
    base = c.store(5.4, 1.85, 1.5, 0.74, "Host baseline", "entropy, rates, fan-out", "slate")
    resp = c.box(1.75, 0.8, 2.5, 0.56, "Layer 4: response", "observe, alert, suspend", "orange", badge=4)
    obs = c.box(4.75, 0.8, 3.05, 0.56, "Dashboard, history, health", "timeline, findings, per-tree view", "orange")

    c.arrow([cfg.l, (plat.r[0], cfg.cy)], dashed=True, color=DASH, label="configures", lpos=(3.95, 7.4))
    c.arrow([(plat.cx, plat.b[1]), (plat.cx, sens.t[1])])
    c.arrow([(sens.cx, sens.b[1]), (sens.cx, bus.t[1])])
    c.arrow([(rules.cx, bus.b[1]), (rules.cx, rules.t[1])])
    c.arrow([(fp.cx, bus.b[1]), (fp.cx, fp.t[1])])
    c.arrow([fp.r, tree.l])
    c.arrow([(fp.cx, fp.b[1]), (fp.cx, score.t[1])])
    c.arrow([tree.b, (tree.cx, score.t[1])])
    c.arrow([(2.35, score.b[1]), (2.35, fuse.t[1])])
    c.arrow([base.t, (base.cx, score.b[1])], dashed=True, color=DASH, label="reads",
            lpos=(base.cx + 0.12, 2.28), lha="left")
    c.arrow([rules.l, (0.42, rules.cy), (0.42, fuse.cy), fuse.l], color=ACCENT, lw=1.6)
    c.ax.text(0.5, 2.55, "override floor", rotation=90, ha="center", va="center", fontsize=6.6,
              fontweight="semibold", color=ACCENT)
    ymid = 1.2
    c.arrow([(fuse.cx - 0.6, fuse.b[1]), (fuse.cx - 0.6, ymid), (resp.cx, ymid), resp.t])
    c.arrow([(fuse.cx - 0.6, ymid), (obs.cx, ymid), obs.t])
    c.legend(0.2, 0.2, [("event and verdict flow", FLOW, False), ("configuration and baseline", DASH, True),
                        ("override floor", ACCENT, False)])
    c.save(out)


def fig3(out):
    """Block diagram: signal path and the two tracks."""
    c = Canvas(6.5, 5.0)
    c.lane(0.05, 0.45, 1.6, 4.9, "Ingest")
    c.lane(1.7, 0.45, 4.78, 4.9, "Accumulate")
    c.lane(4.88, 0.45, 6.45, 4.9, "Decide")
    c1, c2, c3, c4 = 0.83, 2.45, 4.02, 5.66
    ev = c.box(c1, 3.85, 1.34, 0.9, "Event stream", "file, process,\npersistence, decoy", "teal")
    sm = c.box(c1, 2.6, 1.34, 0.9, "Content sampling", "head and tail\nentropy, magic", "teal")
    at = c.box(c1, 1.35, 1.34, 0.9, "Attribution", "host, correlate\nor audit", "yellow")
    ta = c.box(c2, 3.35, 1.3, 0.9, "Track A: window", "decaying ring\nof samples", "purple")
    tb = c.box(c2, 1.35, 1.3, 0.9, "Track B: counters", "cumulative,\nno decay", "purple")
    dv = c.box(c3, 3.35, 1.36, 0.9, "Deviation signals", "z against\nthe baseline", "blue")
    cs = c.box(c3, 1.35, 1.36, 0.9, "Cumulative signals", "novelty, bytes,\nchains", "blue")
    fu = c.box(c4, 2.35, 1.34, 0.9, "Fusion", "noisy-OR\nplus gate", "blue")
    bl = c.store(c4, 3.95, 1.2, 0.8, "Baseline", "from warm-up", "slate")
    bd = c.box(c4, 0.98, 1.34, 0.9 - 0.2, "Level bands", "observe, alert,\nsuspend", "orange")
    c.arrow([ev.b, sm.t])
    c.arrow([sm.b, at.t])
    c.arrow([at.r, tb.l])
    c.arrow([(1.65, at.cy), (1.65, ta.cy), ta.l], dot_start=False)
    c.ax.add_patch(Circle((1.65, at.cy), 0.04, fc=FLOW, ec="none", zorder=3))
    c.arrow([ta.r, dv.l])
    c.arrow([tb.r, cs.l])
    c.arrow([dv.r, (4.83, dv.cy), (4.83, 2.72), (fu.l[0], 2.72)])
    c.arrow([cs.r, (4.83, cs.cy), (4.83, 1.98), (fu.l[0], 1.98)])
    c.arrow([bl.b, fu.t], dashed=True, color=DASH, label="reads", lpos=(c4 + 0.1, 3.05), lha="left")
    c.arrow([fu.b, bd.t])
    c.arrow([(c4, bd.b[1]), (c4, 0.3), (c1, 0.3), at.b], dashed=True, color=ACCENT,
            label="filing policy can change", lpos=(3.3, 0.3), lcolor=ACCENT)
    c.legend(0.2, 0.1, [("signal path", FLOW, False), ("baseline", DASH, True), ("feedback", ACCENT, True)])
    c.save(out)


def fig4(out):
    """Per-event decision sequence."""
    c = Canvas(6.5, 6.6)
    cx = [0.72, 1.96, 3.2, 4.44, 5.68]
    r0, r1, r2, r3, r4 = 5.9, 4.9, 3.6, 2.3, 1.0
    c.lane(0.05, 4.18, 6.45, 6.55, "Measure")
    c.lane(0.05, 0.45, 6.45, 4.08, "Score and respond")
    w, ts, ss = 1.0, 7.6, 6.3
    ev = c.box(cx[0], r1, w, 0.8, "File event\non the bus", "", "teal", tsize=ts, ssize=ss)
    sm = c.box(cx[1], r1, w, 0.8, "Sample\nthe file", "head and tail\nentropy, magic", "teal", tsize=ts, ssize=ss)
    at = c.box(cx[2], r1, w, 0.8, "Attribute\nthe writer", "host, correlate\nor audit", "yellow", tsize=ts, ssize=ss)
    up = c.box(cx[3], r1, w, 0.8, "Update the\nfingerprint", "window append,\ncounters grow", "purple", tsize=ts, ssize=ss)
    d1 = c.diamond(cx[4], r1, 1.04, 0.92, "baseline\nusable?", size=6.8)
    un = c.box(4.95, r0, 2.0, 0.56, "Uncalibrated mode", "absolute write-rate fallback; rules only",
               "slate", dashed=True, tsize=ts, ssize=ss)
    co = c.box(cx[4], r2, w, 0.86, "Compute\n13 signals", "three classes", "blue", tsize=ts, ssize=ss)
    d2 = c.diamond(cx[3], r2, 1.04, 0.92, "any Primary\npresent?", size=6.6)
    fu = c.box(cx[2], r2, w, 0.86, "Fuse the\nevidence", "noisy-OR", "blue", tsize=ts, ssize=ss)
    fl = c.box(cx[1], r2, w, 0.86, "Apply the\noverride floor", "max(level,\nrule severity)", "orange",
               tsize=ts, ssize=ss)
    d3 = c.diamond(cx[1], r3, 1.16, 0.98, "level at or above\nalert level?", size=6.4)
    gt = c.box(cx[3], r3, w, 0.8, "Secondaries\ndo not fuse", "still reported\nas evidence", "slate",
               dashed=True, tsize=ts, ssize=ss)
    al = c.box(cx[0], r3, w, 0.8, "Alert", "one line naming\nthe signals", "red", tsize=ts, ssize=ss)
    ob = c.box(cx[2], r3, w, 0.8, "Observe", "recorded,\nnot alerted", "slate", dashed=True,
               tsize=ts, ssize=ss)
    pb = c.box(cx[1], r4, w + 0.3, 0.7, "Publish the verdict", "dashboard, history,\nhealth", "green",
               tsize=ts, ssize=ss)
    for a, b in [(ev, sm), (sm, at), (at, up), (up, d1)]:
        c.arrow([a.r, b.l])
    c.arrow([d1.b, co.t], label="yes", lpos=(cx[4] + 0.17, 4.2))
    c.arrow([d1.t, un.at("b", cx[4] - 4.95)], dashed=True, color=DASH, label="no", lpos=(cx[4] + 0.17, 5.58))
    c.arrow([un.r, (6.28, un.cy), (6.28, r2), co.r], dashed=True, color=DASH)
    c.arrow([co.l, d2.r])
    c.arrow([d2.l, fu.r], label="yes", lpos=(3.83, r2 + 0.13))
    c.arrow([fu.l, fl.r])
    c.arrow([fl.b, d3.t])
    c.arrow([d2.b, gt.t], dashed=True, color=DASH, label="no", lpos=(cx[3] + 0.17, 2.95))
    c.arrow([d3.l, al.r], label="yes", lpos=(1.33, r3 + 0.13))
    c.arrow([d3.r, ob.l], dashed=True, color=DASH, label="no", lpos=(2.58, r3 + 0.13))
    c.arrow([al.b, (al.cx, r4), pb.l])
    c.arrow([ob.b, (ob.cx, r4), pb.r])
    c.legend(0.2, 0.2, [("evidence continues", FLOW, False), ("fallback or recorded-only path", DASH, True)])
    c.save(out)


def fig5(out):
    """Verdict lifecycle."""
    c = Canvas(6.5, 5.0)
    c.lane(0.05, 3.95, 6.45, 4.9, "Start-up")
    c.lane(0.05, 1.2, 6.45, 3.85, "Scored states")
    c.lane(0.05, 0.4, 6.45, 1.1, "")
    init = (0.5, 4.28)
    c.ax.add_patch(Circle(init, 0.09, fc=INK, ec=INK, zorder=4))
    unc = c.box(2.2, 4.28, 1.7, 0.6, "Uncalibrated", "", "slate", center=True)
    qt = c.box(5.45, 3.15, 1.5, 0.72, "Quiet", "score < 20", "green")
    wa = c.box(5.45, 1.95, 1.5, 0.72, "Watchful", "20 to 45", "yellow")
    su = c.box(3.1, 1.95, 1.5, 0.72, "Suspicious", "45 to 70", "orange")
    al = c.box(1.0, 1.95, 1.5, 0.72, "Alerted", "70 to 100", "red")
    sd = c.box(1.0, 0.72, 1.5, 0.56, "Suspended", "if enabled", "purple")
    fin = (2.7, 0.72)
    c.ax.add_patch(Circle(fin, 0.12, fc="white", ec=INK, lw=1.5, zorder=4))
    c.ax.add_patch(Circle(fin, 0.065, fc=INK, ec=INK, zorder=5))
    c.ax.text(2.95, 0.72, "final: the detector can\nno longer observe\nthis actor", ha="left", va="center",
              fontsize=6.6, color=MUTED, linespacing=1.25)
    c.arrow([(init[0] + 0.09, init[1]), unc.l])
    c.arrow([unc.r, (5.45, 4.28), qt.t], label="baseline ready", lpos=(3.95, 4.28), lsize=6.8)
    c.arrow([qt.b, wa.t], label="evidence", lpos=(5.45 + 0.34, 2.55), lha="left")
    c.arrow([wa.l, su.r], label="rising", lpos=(4.28, 2.08))
    c.arrow([su.l, al.r], label="high", lpos=(2.05, 2.08))
    c.arrow([(0.62, al.b[1]), (0.62, sd.t[1])], label="critical and\nsuspension\nenabled",
            lpos=(0.7, 1.35), lha="left", lsize=6.2)
    c.arrow([sd.r, (fin[0] - 0.12, fin[1])])
    c.arrow([wa.b, (wa.cx, 1.48), (1.5, 1.48), (1.5, al.b[1])], color=ACCENT, lw=1.6,
            label="override floor", lpos=(3.5, 1.48), lcolor=ACCENT)
    c.arrow([al.t, (al.cx, qt.cy), qt.l], dashed=True, color=DASH, label="decay", lpos=(3.1, qt.cy))
    # score ruler
    x0, x1, y0, y1 = 4.35, 6.3, 0.58, 0.82
    k = (x1 - x0) / 100
    segs = [(0, 20, "Quiet", "green"), (20, 45, "Watchful", "yellow"), (45, 70, "Suspicious", "orange"),
            (70, 100, "Alerted", "red")]
    for a, b, name, kind in segs:
        c.ax.add_patch(Rectangle((x0 + a * k, y0), (b - a) * k, y1 - y0, fc=KIND[kind][0], ec=KIND[kind][1],
                                 lw=1.2, zorder=3))
        c.ax.text(x0 + (a + b) / 2 * k, (y0 + y1) / 2, name, ha="center", va="center", fontsize=6.0,
                  fontweight="semibold", color=INK, zorder=5)
    for v in (0, 20, 45, 70, 88, 100):
        c.ax.text(x0 + v * k, 0.47, str(v), ha="center", va="center", fontsize=5.8, color=ACCENT if v == 88 else MUTED,
                  fontweight="bold" if v == 88 else "normal")
    c.ax.plot([x0 + 88 * k] * 2, [y0 - 0.03, y1 + 0.03], color=ACCENT, lw=1.6, zorder=6)
    c.ax.text(x0 + 88 * k, y1 + 0.12, "critical", ha="center", va="center", fontsize=5.8, color=ACCENT,
              fontweight="bold")
    c.legend(0.2, 0.2, [("transition", FLOW, False), ("override floor", ACCENT, False), ("decay", DASH, True)])
    c.save(out)

"""Regenerate every figure in the paper: figures/Figure1.png ... Figure10.png (400 dpi).

    python figures/make_figures.py          # all
    python figures/make_figures.py 1 2      # selected
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import charts  # noqa: E402
import diagrams  # noqa: E402

BUILD = {
    1: diagrams.fig1,
    2: diagrams.fig2,
    3: diagrams.fig3,
    4: diagrams.fig4,
    5: diagrams.fig5,
    6: charts.fig6,
    7: charts.fig7,
    8: charts.fig8,
    9: charts.fig9,
    10: charts.fig10,
}
if __name__ == "__main__":
    which = [int(a) for a in sys.argv[1:]] or sorted(BUILD)
    for n in which:
        BUILD[n](os.path.join(HERE, f"Figure{n}.png"))
        print("wrote", f"Figure{n}.png")

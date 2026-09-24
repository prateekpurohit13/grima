# Third-Party Notices

GRIMA incorporates third-party software and adapts detection logic from published
sources. This file records every such component with its license and copyright holder,
as required by those licenses.

If you add a dependency or adapt detection logic from another project, add an entry here
in the same commit. This is a licensing requirement, not a courtesy.

---

## Go module dependencies

### github.com/fsnotify/fsnotify v1.10.1

**Used for:** cross-platform filesystem event notification (`inotify`, `kqueue`,
`FSEvents`, `ReadDirectoryChangesW`) in `internal/sensor/filewatch`.

**License:** BSD 3-Clause

```
Copyright © 2012 The Go Authors. All rights reserved.
Copyright © fsnotify Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.
* Redistributions in binary form must reproduce the above copyright notice, this
  list of conditions and the following disclaimer in the documentation and/or
  other materials provided with the distribution.
* Neither the name of Google Inc. nor the names of its contributors may be used
  to endorse or promote products derived from this software without specific
  prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Source: https://github.com/fsnotify/fsnotify

---

### github.com/shirou/gopsutil/v4 v4.26.8

**Used for:** process and system telemetry — process creation/exit, process tree,
per-process I/O counters — in `internal/sensor/procwatch`.

**License:** BSD 3-Clause

```
gopsutil is distributed under BSD license reproduced below.

Copyright (c) 2014, WAKAYAMA Shirou
All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

 * Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.
 * Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.
 * Neither the name of the gopsutil authors nor the names of its contributors
   may be used to endorse or promote products derived from this software without
   specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Note: `internal/common/binary.go` within gopsutil is copied and modified from
`golang/encoding/binary.go` (Copyright (c) 2009 The Go Authors, BSD 3-Clause).

Source: https://github.com/shirou/gopsutil

---

### github.com/BurntSushi/toml v1.6.0

**Used for:** configuration file parsing in `internal/config`.

**License:** MIT

```
The MIT License (MIT)

Copyright (c) 2013 TOML authors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

Source: https://github.com/BurntSushi/toml

---

### golang.org/x/sys v0.41.0

**Used for:** indirect dependency of gopsutil (platform syscall bindings).

**License:** BSD 3-Clause

```
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Source: https://cs.opensource.google/go/x/sys

---

## Adapted detection logic

### SigmaHQ rule corpus

**Used for:** the detection logic behind the hard rules `R-SHADOW-DELETE`,
`R-EVENTLOG-CLEAR`, `R-USN-DELETE`, and `R-BACKUP-KILL`, which encode known
pre-encryption and anti-recovery command patterns (`vssadmin delete shadows`,
`wevtutil cl`, `fsutil usn deletejournal`, backup-service termination).

**License:** Detection Rule License (DRL) 1.1 — see
https://github.com/SigmaHQ/sigma/blob/master/LICENSE.Detection.Rules.md

The rules in GRIMA are **independent reimplementations as Go predicates**, not
verbatim copies of Sigma YAML. The command patterns and severity rationale are derived
from the corpus.

Source: https://github.com/SigmaHQ/sigma

---

## Design inspiration (no code or rule text taken)

These works informed the architecture. They are cited in the paper's Related Work
section; no code was copied.

| Source | Contribution to GRIMA's design |
|---|---|
| RansomWall — Shaukat & Ribeiro, COMSNETS 2018 | Layered defense: pre-execution check, decoy files, continuous filesystem monitoring; fusing entropy with additional checks rather than relying on entropy alone |
| UNVEIL — Kharraz et al., USENIX Security 2016 | The kernel-level detection approach and its stability/portability costs, which motivate GRIMA's user-space stance |
| ShieldFS — Continella et al., ACSAC 2016 | Self-healing filesystem semantics; kernel minifilter architecture used as the counterexample |
| Huertas Celdrán et al., *Computers & Security* 2023, DOI `10.1016/j.cose.2023.103510` | Behavioral fingerprinting for ransomware detection |
| EldeRan — Sgandurra et al. 2016 | The fixed-observation-window weakness that motivates the rolling window |
| Davies & Macfarlane, *Sensors* 2022 | Comparison of entropy calculation methods for encrypted-file identification |
| Al-Rimy et al., *Computers & Security* 2018 | Ransomware taxonomy and threat-success-factor survey |

---

## Rejected for licensing or architectural reasons

Recorded so the decision is not re-litigated:

| Project | Reason |
|---|---|
| `nccgroup/KilledProcessCanary` | Kernel driver; violates the user-space constraint |
| `RafWu/RansomWatch` | Windows minifilter driver; violates the user-space constraint |
| Any GPL / AGPL component | Copyleft is viral; incorporating it would relicense the whole project |

---

## Adding an entry

For a **dependency**: name and version, what it is used for, license, the full license
text or a stable link to it, and the copyright holder.

For **adapted logic**: the source project, the license, what was adapted, and an explicit
statement of whether the adaptation is a reimplementation or a copy.

For **inspiration only**: state clearly that no code was taken, so the distinction stays
auditable.

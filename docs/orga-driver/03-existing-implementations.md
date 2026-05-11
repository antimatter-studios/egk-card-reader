# Existing implementations — what's out there

## Open source: nothing usable for ORGA 9xx wire framing

Confirmed by walking these repos / package archives:

| Project | What it has | ORGA support? |
|---------|-------------|---------------|
| [OpenSC/openct](https://github.com/OpenSC/openct) | 18 `ifd-*` drivers (Towitoko, Kaan, CyberJack, GemPC, …) | **No `ifd-orga`** — never written |
| [aqbanking/libchipcard](https://github.com/aqbanking/libchipcard) | CT-API → PC/SC bridge | Wraps proprietary `ctorg*.so` only |
| [LudovicRousseau/CCID](https://github.com/LudovicRousseau/CCID) | All USB-CCID readers | ORGA 9xx is **not** CCID class, so out of scope |
| [Kaupisch-IT/eGK-KVK](https://github.com/Kaupisch-IT/eGK-KVK) | C# eGK reader | P/Invokes vendor `ctorg32.dll`; no framing |
| [cprados/towitoko-linux](https://github.com/cprados/towitoko-linux) | T=1 reference implementation | Towitoko, not ORGA — useful only as a T=1 reference for *card-side* framing |
| gematik GitHub org | Test suites, specs | No terminal-driver code at all |

## Closed source: vendor binaries (the only working stacks)

- **Windows**: `ctorg32.dll` (Ingenico/ORGA Kartensysteme), shipped via "ORGA Treiber"/"ORGA Drivers" installer. Some redistributions: TI-Konnektor installers, Apraxos Windows package.
- **Linux**: `libctorgt1.so` shipped as Debian `.deb` (e.g. `libctorgt1_1.4.7_amd64.deb`). Installed by Apraxos as `/usr/lib/libctorgt1.so`. amd64-only; **no arm64 build**, so even Linux ARM (e.g. Asahi, Raspberry Pi) cannot use the vendor SO directly.
- **macOS**: nothing. No `.dylib` or framework released by Ingenico/Worldline.

Both binaries implement the same CT-API ABI; presumably the same wire framing inside.

## What this leaves us with

Three viable sources for the framing:

1. **USB capture from Windows** running the official driver. Wireshark + USBPcap on a Windows VM (Parallels on this macOS, or VirtualBox) with the real ORGA passed through. Send a known transaction (e.g. RESET CT → REQUEST ICC) and read the wire bytes. Ground truth — no licensing issue, no reverse-engineering of code.
2. **Disassembly of `libctorgt1.so`**. Small ELF, x86-64, plain C, no obfuscation evident. Ghidra/radare2 entry points `CT_init`/`CT_data`/`CT_close`. Beware: clean-room re-implementation rules — we describe behaviour we observed, not bytes we read.
3. **Black-box probing** of the device. Open serial port, try candidate framings (T=1, plain ASCII, STX/ETX), see what responds. Slowest, but doesn't require Windows or RE skills. See [04-plan.md](04-plan.md) Track 3.

Strategy: try (1) and (3) in parallel; fall back to (2) if neither pins it down.

## License hygiene

If we end up clean-rooming from disassembly:
- Document each byte's source: capture, observed behaviour, MKT spec, or inferred-from-pattern.
- Do not paste decompiler output into the repo.
- Do not redistribute the vendor binary.
- Behaviour observations are not subject to copyright; format facts are not subject to copyright. Compiled bytes are.

# Surface snapshot summary

What a controller sees of the reference daemon (`examples/reference-bridge --dump-surface`,
default flags, zero-value surface policies). `make surface` regenerates it; `make surface-check`
fails when it moved. The clusters the endpoint assembler mounts on a bridged endpoint
(Descriptor, BridgedDeviceBasicInformation, Identify, Groups, ScenesManagement) are not part of it.
The effective surface digest is the SHA-256 of the canonical JSON — every id a 0x hex string —
without endpoint numbers, so a change of that encoding moves it as well.

| Metric | Value |
|---|---|
| Effective surface digest | `a6443fbeed979e6949b513fff6361d1982c7c535c0ec42b62eb66c70eaae55df` |
| Policies digest | `fc6822f08d6c5d98a1632f05ea480c2666bc0701ab657f56500011c73e050c24` |
| Specification level | 1.6.1 |
| Endpoints (the root included) | 45 |
| Clusters | 118 |
| Decisions | 577 |
| Withheld | 0 |

## Clusters per endpoint

| Endpoint | Device types | Clusters |
|---|---|---|
| (root) | 0x0016 r5, 0x0012 r1 | 21 |
| `aggregator` | 0x000E r2 | 5 |
| `demo:airquality:1` | 0x002C r1 | 12 |
| `demo:blind:1` | 0x0202 r7 | 1 |
| `demo:button:1` | 0x000F r3 | 1 |
| `demo:ceiling:1` | 0x010C r5 | 3 |
| `demo:colorlight:1` | 0x010D r5 | 3 |
| `demo:contact:1` | 0x0015 r2 | 2 |
| `demo:dishwasher:1` | 0x0075 r2 | 2 |
| `demo:dryer:1` | 0x007C r2 | 2 |
| `demo:electricalmeter:1` | 0x0514 r1 | 3 |
| `demo:electricalmeter:sensor` | 0x0510 r1 | 3 |
| `demo:esa:1` | 0x050D r3 | 2 |
| `demo:evse:1` | 0x050C r2 | 2 |
| `demo:evse:dem` | 0x050D r3 | 2 |
| `demo:evse:meter` | 0x0510 r1 | 3 |
| `demo:evse:power` | 0x0011 r1 | 1 |
| `demo:fan:1` | 0x002B r5 | 1 |
| `demo:flow:1` | 0x0306 r3 | 1 |
| `demo:fridge:1` | 0x0070 r3 | 2 |
| `demo:fridge:cabinet` | 0x0071 r6 | 2 |
| `demo:garage:1` | 0x0230 r1 | 1 |
| `demo:humidity:1` | 0x0307 r3 | 1 |
| `demo:light:1` | 0x0100 r4 | 2 |
| `demo:lock:1` | 0x000A r4 | 1 |
| `demo:meter:1` | 0x0511 r1 | 1 |
| `demo:microwave:1` | 0x0079 r2 | 3 |
| `demo:occupancy:1` | 0x0107 r4 | 1 |
| `demo:oven:1` | 0x007B r2 | 0 |
| `demo:oven:cavity` | 0x0071 r6 | 3 |
| `demo:panel:1` | 0x0231 r1 | 1 |
| `demo:pump:1` | 0x0303 r4 | 3 |
| `demo:purifier:1` | 0x002D r3 | 3 |
| `demo:selector:1` | 0x0027 r1 | 1 |
| `demo:smoke:1` | 0x0076 r2 | 1 |
| `demo:smoke:battery` | 0x0011 r1 | 1 |
| `demo:soil:1` | 0x0045 r1 | 1 |
| `demo:speaker:1` | 0x0022 r1 | 2 |
| `demo:tariff:1` | 0x0513 r1 | 3 |
| `demo:thermometer:1` | 0x0302 r3 | 1 |
| `demo:thermostat:1` | 0x0301 r7 | 3 |
| `demo:vacuum:1` | 0x0074 r4 | 4 |
| `demo:valve:1` | 0x0042 r1 | 1 |
| `demo:washer:1` | 0x0073 r2 | 3 |
| `demo:waterheater:1` | 0x050F r1 | 3 |

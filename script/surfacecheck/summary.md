# Surface snapshot summary

What a controller sees of the reference daemon (`examples/reference-bridge --dump-surface`,
default flags, zero-value surface policies). `make surface` regenerates it; `make surface-check`
fails when it moved. It is the assembled topology: the clusters the endpoint assembler mounts on a
bridged endpoint (Descriptor, BridgedDeviceBasicInformation, Identify, Groups, ScenesManagement,
Binding) and the BridgedNode device type are part of it.
The effective surface digest is the SHA-256 of the canonical JSON — every id a 0x hex string —
without endpoint numbers, so a change of that encoding moves it as well.

| Metric | Value |
|---|---|
| Effective surface digest | `281eaeb4b45f7bc853d3593baf24e7f1e18f024b1d1d41d49c89e442279f31ae` |
| Policies digest | `fc6822f08d6c5d98a1632f05ea480c2666bc0701ab657f56500011c73e050c24` |
| Specification level | 1.6.1 |
| Endpoints (the root included) | 45 |
| Clusters | 240 |
| Decisions | 577 |
| Withheld | 0 |

## Clusters per endpoint

| Endpoint | Device types | Clusters |
|---|---|---|
| (root) | 0x0016 r5, 0x0012 r1 | 21 |
| `aggregator` | 0x000E r2 | 5 |
| `demo:airquality:1` | 0x002C r1, 0x0013 r3 | 15 |
| `demo:blind:1` | 0x0202 r7, 0x0013 r3 | 4 |
| `demo:button:1` | 0x000F r3, 0x0013 r3 | 4 |
| `demo:ceiling:1` | 0x010C r5, 0x0013 r3 | 8 |
| `demo:colorlight:1` | 0x010D r5, 0x0013 r3 | 8 |
| `demo:contact:1` | 0x0015 r2, 0x0013 r3 | 5 |
| `demo:dishwasher:1` | 0x0075 r2, 0x0013 r3 | 5 |
| `demo:dryer:1` | 0x007C r2, 0x0013 r3 | 5 |
| `demo:electricalmeter:1` | 0x0514 r1, 0x0013 r3 | 6 |
| `demo:electricalmeter:sensor` | 0x0510 r1 | 4 |
| `demo:esa:1` | 0x050D r3, 0x0013 r3 | 5 |
| `demo:evse:1` | 0x050C r2, 0x0013 r3 | 5 |
| `demo:evse:dem` | 0x050D r3 | 3 |
| `demo:evse:meter` | 0x0510 r1 | 4 |
| `demo:evse:power` | 0x0011 r1 | 2 |
| `demo:fan:1` | 0x002B r5, 0x0013 r3 | 5 |
| `demo:flow:1` | 0x0306 r3, 0x0013 r3 | 4 |
| `demo:fridge:1` | 0x0070 r3, 0x0013 r3 | 5 |
| `demo:fridge:cabinet` | 0x0071 r6 | 3 |
| `demo:garage:1` | 0x0230 r1, 0x0013 r3 | 4 |
| `demo:humidity:1` | 0x0307 r3, 0x0013 r3 | 4 |
| `demo:light:1` | 0x0100 r4, 0x0013 r3 | 7 |
| `demo:lock:1` | 0x000A r4, 0x0013 r3 | 4 |
| `demo:meter:1` | 0x0511 r1, 0x0013 r3 | 4 |
| `demo:microwave:1` | 0x0079 r2, 0x0013 r3 | 6 |
| `demo:occupancy:1` | 0x0107 r4, 0x0013 r3 | 4 |
| `demo:oven:1` | 0x007B r2, 0x0013 r3 | 3 |
| `demo:oven:cavity` | 0x0071 r6 | 4 |
| `demo:panel:1` | 0x0231 r1, 0x0013 r3 | 4 |
| `demo:pump:1` | 0x0303 r4, 0x0013 r3 | 6 |
| `demo:purifier:1` | 0x002D r3, 0x0013 r3 | 6 |
| `demo:selector:1` | 0x0027 r1, 0x0013 r3 | 4 |
| `demo:smoke:1` | 0x0076 r2, 0x0013 r3 | 4 |
| `demo:smoke:battery` | 0x0011 r1 | 2 |
| `demo:soil:1` | 0x0045 r1, 0x0013 r3 | 4 |
| `demo:speaker:1` | 0x0022 r1, 0x0013 r3 | 5 |
| `demo:tariff:1` | 0x0513 r1, 0x0013 r3 | 6 |
| `demo:thermometer:1` | 0x0302 r3, 0x0013 r3 | 4 |
| `demo:thermostat:1` | 0x0301 r7, 0x0013 r3 | 6 |
| `demo:vacuum:1` | 0x0074 r4, 0x0013 r3 | 7 |
| `demo:valve:1` | 0x0042 r1, 0x0013 r3 | 4 |
| `demo:washer:1` | 0x0073 r2, 0x0013 r3 | 6 |
| `demo:waterheater:1` | 0x050F r1, 0x0013 r3 | 6 |

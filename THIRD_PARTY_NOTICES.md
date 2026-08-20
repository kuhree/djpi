# Third-party notices

## Osmosis

The wireless DJI CompositePack manifest decoder and Action 6 R SDK framing
foundation are adapted from Osmosis:

- Project: <https://github.com/KonradIT/osmosis>
- Reference revision: `a76123441f3c0d476de949da36afdcace310a83e`
- Copyright: Osmosis contributors
- License: MIT

The Action 6 and Pocket 3 test fixtures embedded as base64 text under
`internal/wireless/testdata` are unmodified captures from that revision.
The R SDK framing tests use Osmosis's golden vectors, which Osmosis generated
from DJI's official Osmo GPS Controller Demo.

```text
MIT License

Copyright (c) 2026

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## DJI Osmo GPS Controller Demo

DJI's upstream repository documents the R SDK protocol under DJI's End User
License Agreement and offers its Osmo GPS Controller Demo code under the MIT
license below. djpi does not redistribute the upstream documents or source.

- Project: <https://github.com/dji-sdk/Osmo-GPS-Controller-Demo>
- Reference revision: `92fe23e5a749f189593f980a26a105c3bb66aa1c`
- DJI EULA: <https://developer.dji.com/policies/eula/>

```text
The MIT License (MIT)
Copyright (c) 2025 SZ DJI Technology Co., Ltd.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

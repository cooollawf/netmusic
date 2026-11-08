# Third party components

`netmusic` itself is licensed under the MIT license (see `LICENSE`). The two Go
modules it links are independent works with their own licenses; they are **not**
relicensed by this project.

| Module | Version | License | Copyright |
|---|---|---|---|
| [`github.com/hajimehoshi/go-mp3`](https://github.com/hajimehoshi/go-mp3) | v0.3.4 | ISC | Copyright (c) 2013 Hajime Hoshi |
| [`github.com/skip2/go-qrcode`](https://github.com/skip2/go-qrcode) | v0.0.0-20200617195104-da1b6568686e | MIT | Copyright (c) 2014 Tom Harwood |

## go-mp3 (ISC)

```
Copyright (c) 2013 Hajime Hoshi

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND
FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
```

## go-qrcode (MIT)

```
Copyright (c) 2014 Tom Harwood

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

## Not bundled

Nothing else is linked. In particular the project does **not** vendor
go-musicfox or any NetEase API SDK: the HTTP client, the weapi encryption and
the WASAPI bindings are written from scratch in this repository, using only the
Go standard library.

## Music data

Song metadata, lyrics, comments and audio streams are fetched from NetEase Cloud
Music at runtime and remain the property of NetEase and the respective rights
holders. No music data is included in this repository.

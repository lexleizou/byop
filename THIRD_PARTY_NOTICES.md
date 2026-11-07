# Third-Party Notices

## BYOP Logo

Assets: `assets/byop-logo.svg` and `assets/byop-logo-1024.png`
Description: The filled circle with a cutout π is a project-original BYOP
asset. The Lucide `Pi` mark remains available for non-brand icon use.

## MyGo

Source: <https://github.com/egoist/mygo>
Version: `v0.5.5`
License: MIT

Copyright (c) 2026 MyGo contributors

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

## Onevra

Source: <https://github.com/oneo-me/onevra>
Go module: `github.com/oneo-me/onevra/source v0.2026.13`
Referenced commit: `ece5cd99c334703f7878209f77cc24af14a49b79`
Usage: Chat components and bundled Lucide icon definitions are linked as a Go
dependency.
License: MIT

Copyright (c) 2026 ONEO

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

## Lucide

Source: <https://github.com/lucide-icons/lucide>
Asset: `Pi` icon geometry in `assets/pi.svg`; remaining inline SVG icons follow
Lucide geometry and stroke rules. Chat icons are used through Onevra's bundled
Lucide package.
License: ISC

Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as part
of Feather (MIT). All other copyright (c) for Lucide are held by Lucide
Contributors 2022-2026.

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.

## Lobe Icons

Source: <https://github.com/lobehub/lobe-icons>
Package: `@lobehub/icons-static-svg@1.98.0`
Assets: `assets/brands/deepseek.svg`, `assets/brands/claude.svg` and
`assets/brands/qwen.svg`
Usage: Embedded model-brand marks in the model picker and composer control.
License: MIT

Copyright (c) 2023 LobeHub

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

The bundled license text is also available at `assets/brands/LICENSE`.

## Radix Colors

Source: <https://github.com/radix-ui/colors>
Referenced commit: `dbdb85470547c7d34b9001f48fddb08ded335979`
Usage: Palette values and role guidance used to derive the BYOP product color
tokens in `design-system/pi-workbench/tokens.json`. BYOP pins those values and
does not follow upstream color changes automatically.
License: MIT

MIT License

Copyright (c) 2021-2022 Modulz
Copyright (c) 2022-Present WorkOS

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

## Pele (Mermaid Diagram Plugin)

Source: <https://github.com/obsidianmd/pele>
Version: `pele@1.7.0` (commit `8603bf3313add9143cd417015d4a6bd6c522dada`)
Usage: The separately installable `plugins/mermaid-diagram` ships the original ESM renderer for offline SVG previews. It is not linked into the Go binary. The P30 inline diagram card additionally embeds read-only copies of `web/diagram-renderer.mjs` and `web/vendor/pele.mjs` into the Go binary (served over `byop-diagram://`); the embedded files are byte-identical to the plugin copies, the plugin directory itself is not modified, and the plugin remains optional.
License: MIT; upstream Lucide ISC notice retained. Full license text: [plugins/mermaid-diagram/web/vendor/LICENSE](plugins/mermaid-diagram/web/vendor/LICENSE). Source and checksums: [vendor/README.md](plugins/mermaid-diagram/web/vendor/README.md).

## Inter Font

Source: <https://github.com/rsms/inter>
Version: `v4.1` (`Inter-4.1.zip`, SHA-256 `9883fdd4…b11e`), files from `extras/ttf/`
Usage: The BoardUI appearance preset (P31) embeds `Inter-Regular/Medium/SemiBold/Bold.ttf` from `assets/fonts/` into the Go binary and registers them as "BYOP Inter". The font files are not modified.
Copyright: Copyright (c) 2016 The Inter Project Authors (https://github.com/rsms/inter)
License: SIL Open Font License, Version 1.1; full text at `assets/fonts/OFL-Inter.txt` (also shipped to `Contents/Resources`).

## Noto Sans SC Font

Source: <https://github.com/notofonts/noto-cjk>
Version: `Sans2.004` (`18_NotoSansSC.zip`, SHA-256 `4d107c09…292ec`)
Usage: The BoardUI appearance preset (P31) embeds `NotoSansSC-Regular/Medium/Bold.otf` from `assets/fonts/` into the Go binary and registers them as "BYOP Noto Sans SC". The font files are not modified.
Copyright: © 2014-2021 Adobe (http://www.adobe.com/)
Trademark: Noto is a trademark of Google LLC.
License: SIL Open Font License, Version 1.1; full text at `assets/fonts/OFL-NotoSansSC.txt` (also shipped to `Contents/Resources`).

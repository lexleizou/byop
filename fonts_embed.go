package main

import (
	_ "embed"
)

// P31 §1: the BoardUI preset embeds Inter (Latin, digits) and Noto Sans SC
// (Chinese) under the SIL Open Font License 1.1. The files are embedded
// unconditionally so a single binary carries every appearance; only the
// BoardUI preset registers them into Core Text. The OFL 1.1 license texts
// live in assets/fonts/ and ship to Contents/Resources via mygo.json's
// resources (see THIRD_PARTY_NOTICES.md); the embedded fonts' name tables
// also carry the license text, as OFL 2 requires.

//go:embed assets/fonts/Inter-Regular.ttf
var fontInterRegular []byte

//go:embed assets/fonts/Inter-Medium.ttf
var fontInterMedium []byte

//go:embed assets/fonts/Inter-SemiBold.ttf
var fontInterSemiBold []byte

//go:embed assets/fonts/Inter-Bold.ttf
var fontInterBold []byte

//go:embed assets/fonts/NotoSansSC-Regular.otf
var fontNotoSansSCRegular []byte

//go:embed assets/fonts/NotoSansSC-Medium.otf
var fontNotoSansSCMedium []byte

//go:embed assets/fonts/NotoSansSC-Bold.otf
var fontNotoSansSCBold []byte

package assets

import (
	_ "embed"
)

//go:embed pattern_1080p.h264
var Pattern1080pH264 []byte

//go:embed pattern_360p.h264
var Pattern360pH264 []byte

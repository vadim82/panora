//go:build !unix

package omarchytheme

import (
	"os"
	"strconv"
)

func directoryIdentity(info os.FileInfo) string {
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

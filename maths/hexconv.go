package maths

import (
	"strconv"
	"strings"
)

func HexToInt(hex string) (int64, error) {
	hex = strings.Replace(hex, "0x", "", -1)
	hex = strings.Replace(hex, "0X", "", -1)

	return strconv.ParseInt(hex, 16, 64)
}

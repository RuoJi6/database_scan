package textfix

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

const (
	EncodingAuto        = "auto"
	EncodingUTF8        = "utf8"
	EncodingGBK         = "gbk"
	EncodingGB18030     = "gb18030"
	EncodingBig5        = "big5"
	EncodingShiftJIS    = "shift-jis"
	EncodingEucKR       = "euc-kr"
	EncodingLatin1      = "latin1"
	EncodingWindows1252 = "windows-1252"
)

var supportedEncodings = map[string]bool{
	EncodingAuto: true, EncodingUTF8: true, EncodingGBK: true, EncodingGB18030: true,
	EncodingBig5: true, EncodingShiftJIS: true, EncodingEucKR: true,
	EncodingLatin1: true, EncodingWindows1252: true,
}

func NormalizeEncoding(name string) string {
	key := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(name, "_", "-")))
	switch key {
	case "", "auto", "自动":
		return EncodingAuto
	case "utf-8", "utf8":
		return EncodingUTF8
	case "gbk", "cp936", "gb2312":
		return EncodingGBK
	case "gb18030", "gb-18030":
		return EncodingGB18030
	case "big5", "big-5", "cp950":
		return EncodingBig5
	case "shift-jis", "shiftjis", "sjis", "cp932":
		return EncodingShiftJIS
	case "euc-kr", "euckr", "ksc5601", "cp949":
		return EncodingEucKR
	case "latin1", "latin-1", "iso-8859-1":
		return EncodingLatin1
	case "windows-1252", "windows1252", "win1252", "cp1252":
		return EncodingWindows1252
	default:
		return key
	}
}

func IsSupportedEncoding(name string) bool {
	return supportedEncodings[NormalizeEncoding(name)]
}

func SupportedEncodings() []string {
	return []string{EncodingAuto, EncodingUTF8, EncodingGBK, EncodingGB18030, EncodingBig5, EncodingShiftJIS, EncodingEucKR, EncodingLatin1, EncodingWindows1252}
}

// RepairMojibake restores strings that were UTF-8 bytes misread as MySQL latin1.
// Normal UTF-8 text is returned unchanged.
func RepairMojibake(s string) string {
	return RepairString(s, EncodingAuto)
}

func RepairString(s, selectedEncoding string) string {
	if s == "" || !looksLikeMojibake(s) {
		return s
	}
	raw, ok := mysqlLatin1Bytes(s)
	if !ok {
		return s
	}
	encodingName := NormalizeEncoding(selectedEncoding)
	if encodingName == EncodingAuto {
		return bestRepair(s, raw)
	}
	recovered, ok := decodeRaw(raw, encodingName)
	if !ok || scoreReadableCJK(recovered) <= scoreReadableCJK(s) {
		return s
	}
	return recovered
}

func RepairBytes(raw []byte, selectedEncoding string) string {
	if len(raw) == 0 {
		return ""
	}
	encodingName := NormalizeEncoding(selectedEncoding)
	if encodingName != EncodingAuto {
		if recovered, ok := decodeRaw(raw, encodingName); ok {
			return recovered
		}
		return string(raw)
	}
	if utf8.Valid(raw) {
		return RepairString(string(raw), EncodingAuto)
	}
	return bestRepair(string(raw), raw)
}

func bestRepair(original string, raw []byte) string {
	if candidate, ok := decodeRaw(raw, EncodingUTF8); ok && scoreReadableCJK(candidate) > scoreReadableCJK(original) {
		return candidate
	}
	best := original
	bestScore := scoreReadableCJK(original)
	for _, name := range []string{EncodingGBK, EncodingGB18030, EncodingBig5, EncodingShiftJIS, EncodingEucKR, EncodingWindows1252, EncodingLatin1} {
		candidate, ok := decodeRaw(raw, name)
		if !ok {
			continue
		}
		score := scoreReadableCJK(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func decodeRaw(raw []byte, name string) (string, bool) {
	switch NormalizeEncoding(name) {
	case EncodingUTF8:
		if !utf8.Valid(raw) {
			return "", false
		}
		return string(raw), true
	case EncodingGBK:
		return decodeWith(raw, simplifiedchinese.GBK)
	case EncodingGB18030:
		return decodeWith(raw, simplifiedchinese.GB18030)
	case EncodingBig5:
		return decodeWith(raw, traditionalchinese.Big5)
	case EncodingShiftJIS:
		return decodeWith(raw, japanese.ShiftJIS)
	case EncodingEucKR:
		return decodeWith(raw, korean.EUCKR)
	case EncodingLatin1:
		return decodeWith(raw, charmap.ISO8859_1)
	case EncodingWindows1252:
		return decodeWith(raw, charmap.Windows1252)
	default:
		return "", false
	}
}

func decodeWith(raw []byte, enc encoding.Encoding) (string, bool) {
	reader := transform.NewReader(bytes.NewReader(raw), enc.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil || !utf8.Valid(decoded) {
		return "", false
	}
	return string(decoded), true
}

func mysqlLatin1Bytes(s string) ([]byte, bool) {
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		if r <= 0xff {
			raw = append(raw, byte(r))
			continue
		}
		if b, ok := windows1252Byte(r); ok {
			raw = append(raw, b)
			continue
		}
		return nil, false
	}
	return raw, true
}

func windows1252Byte(r rune) (byte, bool) {
	switch r {
	case '€':
		return 0x80, true
	case '‚':
		return 0x82, true
	case 'ƒ':
		return 0x83, true
	case '„':
		return 0x84, true
	case '…':
		return 0x85, true
	case '†':
		return 0x86, true
	case '‡':
		return 0x87, true
	case 'ˆ':
		return 0x88, true
	case '‰':
		return 0x89, true
	case 'Š':
		return 0x8a, true
	case '‹':
		return 0x8b, true
	case 'Œ':
		return 0x8c, true
	case 'Ž':
		return 0x8e, true
	case '‘':
		return 0x91, true
	case '’':
		return 0x92, true
	case '“':
		return 0x93, true
	case '”':
		return 0x94, true
	case '•':
		return 0x95, true
	case '–':
		return 0x96, true
	case '—':
		return 0x97, true
	case '˜':
		return 0x98, true
	case '™':
		return 0x99, true
	case 'š':
		return 0x9a, true
	case '›':
		return 0x9b, true
	case 'œ':
		return 0x9c, true
	case 'ž':
		return 0x9e, true
	case 'Ÿ':
		return 0x9f, true
	default:
		return 0, false
	}
}

func looksLikeMojibake(s string) bool {
	markers := 0
	for _, r := range s {
		switch r {
		case 'Ã', 'Â', 'Ä', 'Å', 'Æ', 'Ç', 'È', 'É', 'Ê', 'å', 'ä', 'æ', 'ç', 'è', 'é', 'Œ', 'œ', 'µ', '¯', '€', '™', 'ž', 'Ÿ', '¡', '¢', '£', '¤', '¥', '¦', '§', '¨', '©', 'ª', '«', '¬', '®', '°', '±', '²', '³', '´', '¶', '·', '¸', '¹', 'º', '»', '¼', '½', '¾', '¿':
			markers++
		}
		if markers >= 2 {
			return true
		}
	}
	return false
}

func scoreReadableCJK(s string) int {
	score := 0
	for _, r := range s {
		if isCJK(r) {
			score += 3
			continue
		}
		if strings.ContainsRune("ÃÂÄÅÆÇÈÉÊåäæçèéŒœµ€™", r) {
			score--
		}
	}
	return score
}

func isCJK(r rune) bool {
	return (r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0xF900 && r <= 0xFAFF)
}

package modelvendor

import "strings"

// Detect 推断模型名所属的厂商 ID，无法判定时返回空串。
func Detect(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return ""
	}

	segments := strings.Split(normalized, "/")
	base := strings.TrimSpace(segments[len(segments)-1])
	for _, segment := range segments[:len(segments)-1] {
		if vendor, ok := prefixAliases[strings.TrimSpace(segment)]; ok {
			return vendor
		}
	}

	if vendor := matchNamePattern(base); vendor != "" {
		return vendor
	}

	if vendor, ok := lookupIndex(normalized); ok {
		return vendor
	}
	if vendor, ok := lookupIndex(base); ok {
		return vendor
	}
	return ""
}

func matchNamePattern(name string) string {
	if name == "" {
		return ""
	}
	for _, pattern := range namePatterns {
		for _, token := range pattern.tokens {
			if matchToken(name, token) {
				return pattern.vendor
			}
		}
	}
	return ""
}

// matchToken 判断模型名是否以该 token 开头。短 token 额外要求后续字符不是字母，
// 否则 "yi" 之类的两字母前缀会误伤大量无关模型。
func matchToken(name, token string) bool {
	if !strings.HasPrefix(name, token) {
		return false
	}
	rest := name[len(token):]
	if rest == "" || len(token) > shortTokenMaxLen {
		return true
	}
	next := rest[0]
	return next < 'a' || next > 'z'
}

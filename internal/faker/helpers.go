package faker

import (
	"fmt"
	"maps"
	"net"
	"strings"
	"unicode"
)

func normalizeFilterName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "\"", "")
	return value
}

func parseList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := normalizeFilterName(part)
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func normalizeFakeFunctionName(name string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func buildFakeFunctionConfig(functionName string, params []string) string {
	if len(params) == 0 {
		return functionName
	}
	parts := append([]string{functionName}, params...)
	return strings.Join(parts, ";")
}

func extractJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return ""
	}
	return raw[start : end+1]
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cloned := make(map[string]string, len(m))
	maps.Copy(cloned, m)
	return cloned
}

func makeStringSet(ss []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		set[s] = struct{}{}
	}
	return set
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

func buildSelector(schema, table, column string) string {
	return fmt.Sprintf("%s.%s.%s", normalizeFilterName(schema), normalizeFilterName(table), normalizeFilterName(column))
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

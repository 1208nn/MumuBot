package utils

import "strconv"

// ParseInt64Value 将常见 JSON/OneBot 数值类型转换为 int64。
func ParseInt64Value(v any) (int64, bool) {
	switch value := v.(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		return int64(value), true
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

package schedule

import (
	"fmt"
	"strings"
	"time"
)

// ParseDateRange 解析 "MM/DD-MM/DD" 或单个 "MM/DD"（单个时 end=start）。
// 无年份 → 按 now 推断（跨年：若结果早于 now 半年以上则视为次年，规范 §15.4 时区口径）。
func ParseDateRange(s string, now time.Time, loc *time.Location) (start, end string, err error) {
	s = strings.TrimSpace(s)
	var a, b string
	if i := strings.Index(s, "-"); i >= 0 {
		a, b = s[:i], s[i+1:]
	} else {
		a, b = s, s
	}
	sd, err := parseMonthDay(a, now, loc)
	if err != nil {
		return "", "", err
	}
	ed, err := parseMonthDay(b, now, loc)
	if err != nil {
		return "", "", err
	}
	return sd.Format("2006-01-02"), ed.Format("2006-01-02"), nil
}

func parseMonthDay(s string, now time.Time, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	// 支持 MM/DD 与中式 M月D日
	s = strings.NewReplacer("月", "/", "日", "", "号", "").Replace(s)
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("非法日期: %q（应为 MM/DD）", s)
	}
	var m, d int
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &m); err != nil {
		return time.Time{}, fmt.Errorf("非法月份: %q", parts[0])
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &d); err != nil {
		return time.Time{}, fmt.Errorf("非法日期: %q", parts[1])
	}
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, fmt.Errorf("日期越界: %d/%d", m, d)
	}
	year := now.In(loc).Year()
	cand := time.Date(year, time.Month(m), d, 0, 0, 0, 0, loc)
	// 跨年推断：早于 now 半年以上 → 次年
	if cand.Before(now.In(loc).AddDate(0, -6, 0)) {
		cand = cand.AddDate(1, 0, 0)
	}
	return cand, nil
}

// shiftDate 把 "YYYY-MM-DD" 平移 n 天。
func shiftDate(ymd string, n int, loc *time.Location) (string, error) {
	t, err := time.ParseInLocation("2006-01-02", ymd, loc)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, 0, n).Format("2006-01-02"), nil
}

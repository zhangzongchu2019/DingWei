// Package clock 提供可注入的时钟（便于测试时间相关逻辑：日程提醒/归档/对账）。
package clock

import "time"

// Clock 时钟接口。生产用 Real，测试用 Fake。
type Clock interface {
	Now() time.Time
}

// Real 真实时钟。
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

// Fake 可注入时钟（测试用）。
type Fake struct{ T time.Time }

func (f *Fake) Now() time.Time { return f.T }

// Advance 推进 fake 时间。
func (f *Fake) Advance(d time.Duration) { f.T = f.T.Add(d) }

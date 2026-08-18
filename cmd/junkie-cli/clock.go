package main

import "time"

// countdown is how every screen here tells the time left in a phase.
//
// It anchors on the server's own secondsLeft plus elapsed monotonic time,
// rather than on the deadline in wall-clock terms. Two things fall out of
// that: skew between this machine's clock and the server's cannot drift the
// display, and an NTP correction mid-block cannot jump it — time.Since reads
// the monotonic clock, which no adjustment touches.
type countdown struct {
	baseSeconds int
	fetchedAt   time.Time
}

func newCountdown(seconds int) countdown {
	return countdown{baseSeconds: seconds, fetchedAt: time.Now()}
}

// reset re-anchors on a fresh reading from the server.
func (c *countdown) reset(seconds int) {
	c.baseSeconds = seconds
	c.fetchedAt = time.Now()
}

// remaining floors at zero: past the deadline the server simply has not been
// asked to transition yet, and a negative countdown would read as a fault.
func (c countdown) remaining() int {
	left := c.baseSeconds - int(time.Since(c.fetchedAt).Seconds())
	if left < 0 {
		return 0
	}
	return left
}

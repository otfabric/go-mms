// SPDX-License-Identifier: MIT

package pdu

import (
	"errors"
	"fmt"
	"time"
)

var errGeneralizedTime = errors.New("not a GeneralizedTime")

// ParseGeneralizedTime parses the GeneralizedTime forms of X.680 clause
// 46 (ISO 8601 basic format): YYYYMMDDHH, optionally followed by MM and
// SS, a fraction (. or ,) of the last component present, and either Z,
// a ±hh[mm] offset from UTC, or nothing for local time. Local time has
// no known offset on the wire and is read as UTC. The result is in UTC.
func ParseGeneralizedTime(s string) (time.Time, error) {
	t, err := parseGeneralizedTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q", errGeneralizedTime, s)
	}
	return t, nil
}

func parseGeneralizedTime(s string) (time.Time, error) {
	p := 0
	num := func(n int) (int, bool) {
		if p+n > len(s) {
			return 0, false
		}
		v := 0
		for _, c := range []byte(s[p : p+n]) {
			if c < '0' || c > '9' {
				return 0, false
			}
			v = v*10 + int(c-'0')
		}
		p += n
		return v, true
	}
	isDigit := func() bool { return p < len(s) && s[p] >= '0' && s[p] <= '9' }

	year, ok1 := num(4)
	month, ok2 := num(2)
	day, ok3 := num(2)
	hour, ok4 := num(2)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return time.Time{}, errGeneralizedTime
	}
	// unit is the length of the last component present, for a fraction.
	unit := time.Hour
	minute, sec := 0, 0
	if isDigit() {
		m, ok := num(2)
		if !ok {
			return time.Time{}, errGeneralizedTime
		}
		minute, unit = m, time.Minute
		if isDigit() {
			sv, ok := num(2)
			if !ok {
				return time.Time{}, errGeneralizedTime
			}
			sec, unit = sv, time.Second
		}
	}
	var frac time.Duration
	if p < len(s) && (s[p] == '.' || s[p] == ',') {
		p++
		start := p
		f, scale := 0.0, 0.1
		for isDigit() {
			f += float64(s[p]-'0') * scale
			scale /= 10
			p++
		}
		if p == start {
			return time.Time{}, errGeneralizedTime
		}
		frac = time.Duration(f*float64(unit) + 0.5)
	}
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || sec > 59 {
		return time.Time{}, errGeneralizedTime
	}
	offset := 0
	switch {
	case p == len(s): // local time, read as UTC
	case s[p] == 'Z':
		p++
	case s[p] == '+' || s[p] == '-':
		sign := 1
		if s[p] == '-' {
			sign = -1
		}
		p++
		oh, ok := num(2)
		if !ok || oh > 23 {
			return time.Time{}, errGeneralizedTime
		}
		om := 0
		if p < len(s) {
			if om, ok = num(2); !ok || om > 59 {
				return time.Time{}, errGeneralizedTime
			}
		}
		offset = sign * (oh*3600 + om*60)
	default:
		return time.Time{}, errGeneralizedTime
	}
	if p != len(s) {
		return time.Time{}, errGeneralizedTime
	}
	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.FixedZone("", offset))
	if t.Day() != day {
		return time.Time{}, errGeneralizedTime // e.g. 20240230
	}
	return t.Add(frac).UTC(), nil
}

package server

import (
	"encoding/json"
	"strings"
	"time"
)

// FlexibleTime unmarshals updatedAt-style fields from either:
// - RFC3339 / RFC3339Nano strings (JSON string)
// - Unix milliseconds (JSON number)
// - Unix seconds (JSON number)
// - Apple reference date seconds/milliseconds (JSON number): seconds since 2001-01-01T00:00:00Z
//
// This fixes: Time.UnmarshalJSON: input is not a JSON string
type FlexibleTime struct {
	time.Time
}

func (ft *FlexibleTime) UnmarshalJSON(data []byte) error {
	if ft == nil {
		return nil
	}
	s := strings.TrimSpace(string(data))
	if s == "null" || s == "" {
		ft.Time = time.Time{}
		return nil
	}

	// JSON string → RFC3339 variants
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		str = strings.TrimSpace(str)
		if str == "" {
			ft.Time = time.Time{}
			return nil
		}
		t, err := time.Parse(time.RFC3339Nano, str)
		if err != nil {
			t, err = time.Parse(time.RFC3339, str)
		}
		if err != nil {
			return err
		}
		ft.Time = t.UTC()
		return nil
	}

	// JSON number → Unix ms/seconds, or Apple reference seconds/ms.
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	f, err := n.Float64()
	if err != nil {
		return err
	}

	// Apple "reference date" epoch starts at 2001-01-01T00:00:00Z.
	// Offset between Unix epoch (1970) and Apple epoch (2001) is 978307200 seconds.
	const appleEpochUnixSeconds = 978307200
	const appleEpochUnixMillis = appleEpochUnixSeconds * 1000

	// Heuristics:
	// - Very large values are milliseconds.
	// - For smaller values, disambiguate Apple-reference vs Unix by magnitude:
	//   any timestamp < appleEpochUnixSeconds (978307200) is almost certainly Apple reference seconds.
	//   similarly for milliseconds < appleEpochUnixMillis.
	//
	// This keeps JS Date.now() (unix ms ~ 1.7e12) working, and fixes iOS Date "timeIntervalSinceReferenceDate"
	// (apple seconds ~ 7-9e8 today) without requiring clients to change.
	if f >= 1e11 { // milliseconds (either unix or apple)
		ms := int64(f)
		rem := f - float64(ms)
		if ms < appleEpochUnixMillis {
			ft.Time = time.Unix(appleEpochUnixSeconds, 0).Add(time.Duration(f * float64(time.Millisecond))).UTC()
			return nil
		}
		ft.Time = time.UnixMilli(ms).Add(time.Duration(rem * float64(time.Millisecond))).UTC()
		return nil
	}

	sec := int64(f)
	frac := f - float64(sec)
	nsec := int64(frac * 1e9)
	if sec < appleEpochUnixSeconds {
		ft.Time = time.Unix(appleEpochUnixSeconds, 0).Add(time.Duration(f * float64(time.Second))).UTC()
		return nil
	}
	ft.Time = time.Unix(sec, nsec).UTC()
	return nil
}

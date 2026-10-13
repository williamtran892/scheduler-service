package endpoint

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"scheduler/internal/platform"
	"scheduler/internal/scheduling"
)

const channelHeader = "X-Channel"

// channelOf reads the calling channel from X-Channel, the stand-in for the token's channel claim
// until authentication exists: missing is 400, unknown is 422.
func channelOf(r *http.Request) (scheduling.Channel, error) {
	v := r.Header.Get(channelHeader)
	if v == "" {
		return "", platform.BadRequest(channelHeader, "header is required")
	}
	ch, err := scheduling.ParseChannel(v)
	if err != nil {
		return "", platform.InvalidValue(channelHeader, fmt.Sprintf("unknown channel %q", v))
	}
	return ch, nil
}

// parseUUID requires a UUID in its canonical hyphenated form and returns it lower-cased.
func parseUUID(field, value string) (string, error) {
	if value == "" {
		return "", platform.BadRequest(field, "is required")
	}
	id, ok := canonicalUUID(value)
	if !ok {
		return "", platform.BadRequest(field, "must be a UUID")
	}
	return id, nil
}

func canonicalUUID(s string) (string, bool) {
	u, err := uuid.Parse(s)
	if err != nil || len(s) != len(u.String()) {
		return "", false
	}
	return u.String(), true
}

// parseInstant reads field as an RFC 3339 date-time, which carries an offset.
func parseInstant(field, value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, platform.BadRequest(field, "must be an ISO 8601 date-time with offset")
	}
	return t, nil
}

// parseOptionalInstant reads an RFC 3339 date-time with an offset; empty means not sent.
func parseOptionalInstant(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := parseInstant(field, value)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// parseInteger reads a JSON number that must be an integer. Missing (or null) is 400 when
// required and nil otherwise; a string, a fraction or anything else is 400.
func parseInteger(field string, raw json.RawMessage, required bool) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return nil, platform.BadRequest(field, "is required")
		}
		return nil, nil
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil {
		return nil, platform.BadRequest(field, "must be an integer")
	}
	return &n, nil
}

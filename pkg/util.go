package main

import (
	"strings"
	"time"
)

func parseInputTime(t string) (time.Time, error) {
	//check if a negative duration.
	var T time.Time
	if strings.HasPrefix(t, "-") {
		//parse as duration
		dur, err := time.ParseDuration(t)
		if err != nil {
			return T, err
		}
		return time.Now().Add(dur), nil
	}
	T, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return T, err
	}
	return T, nil
}

/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */
package errors

import (
	"strings"
)

const (
	MissingParam        = "missing parameter"
	InvalidParam        = "invalid parameter"
	ConnectionError     = "connection error"
	ConfigError         = "configuration error"
	NoMetricsError      = "no metrics"
	NoInstancesError    = "no instances"
	NoCollectorsError   = "no collectors"
	ApiResponse         = "error reading api response"
	ApiResponseError    = "api request rejected"
	DLoadError          = "dynamic load error"
	ImplementationError = "implementation error"
	ScheduleError       = "schedule error"
)

type Error struct {
	class string
	msg   string
}

func (e Error) Error() string {
	return e.class + " => " + e.msg
}

func New(class, msg string) Error {
	return Error{class: class, msg: msg}
}

func GetClass(err error) string {
	e := strings.Split(err.Error(), " => ")
	if len(e) > 1 {
		return e[0]
	}
	return ""
}

func IsErr(err error, class string) bool {
	// dirty solution, temporarily
	return strings.Contains(GetClass(err), class)
}

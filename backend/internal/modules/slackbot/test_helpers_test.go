package slackbot

import (
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func assertHTTPError(t *testing.T, err error, expectedCode int) {
	t.Helper()

	if assert.Error(t, err) {
		httpErr, ok := err.(*echo.HTTPError)
		if assert.True(t, ok, "expected echo.HTTPError") {
			assert.Equal(t, expectedCode, httpErr.Code)
		}
	}
}

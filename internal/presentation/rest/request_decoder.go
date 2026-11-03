package rest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const maxRequestBodyBytes = 64 << 10

var (
	errMalformedRequest     = errors.New("malformed request")
	errPayloadTooLarge      = errors.New("payload too large")
	errUnsupportedMediaType = errors.New("unsupported media type")
)

func decodeJSON(c *gin.Context, dst any) error {
	if c.ContentType() != "application/json" {
		return errUnsupportedMediaType
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return decodeFailure(err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return decodeFailure(err)
	}
	return nil
}

func decodeFailure(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errPayloadTooLarge
	}
	return errMalformedRequest
}

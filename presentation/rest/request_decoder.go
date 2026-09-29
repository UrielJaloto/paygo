package rest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const maximumRequestBodyBytes = 64 << 10

var (
	errMalformedRequest     = errors.New("malformed request")
	errPayloadTooLarge      = errors.New("payload too large")
	errUnsupportedMediaType = errors.New("unsupported media type")
)

func decodeJSON(c *gin.Context, destination any) error {
	if c.ContentType() != "application/json" {
		return errUnsupportedMediaType
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maximumRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return classifyDecodeError(err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return classifyDecodeError(err)
	}
	return nil
}

func classifyDecodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errPayloadTooLarge
	}
	return errMalformedRequest
}
